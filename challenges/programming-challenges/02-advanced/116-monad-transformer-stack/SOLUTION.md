# Solution: Monad Transformer Stack

## Architecture Overview

The solution is structured in five layers:

1. **Core traits** -- `Monad` and `Monoid` using GATs to simulate higher-kinded types
2. **Base monads** -- `Identity<T>`, `Maybe<T>`, `Outcome<T, E>` with full Monad implementations
3. **Transformers** -- `ReaderT<Env, Base>`, `WriterT<W, Base>`, `StateT<S, Base>` that add effects to any base monad
4. **Composed stack** -- a practical three-transformer stack combining config, logging, and errors
5. **Application example** -- user registration pipeline demonstrating real-world usage

The critical design decision is how to represent transformer computations. Since Rust closures have unnameable types, transformers store boxed closures (`Box<dyn FnOnce(...)>`). This adds one heap allocation per monadic bind but makes the API ergonomic and composable.

## Rust Solution

### Project Setup

```bash
cargo new monad-transformers
cd monad-transformers
```

```toml
[package]
name = "monad-transformers"
version = "0.1.0"
edition = "2021"
```

### Source: `src/traits.rs`

```rust
/// Monoid: a type with an identity element and an associative combine operation.
pub trait Monoid: Clone {
    fn empty() -> Self;
    fn combine(&mut self, other: Self);
}

impl<T: Clone> Monoid for Vec<T> {
    fn empty() -> Self {
        Vec::new()
    }
    fn combine(&mut self, other: Self) {
        self.extend(other);
    }
}

impl Monoid for String {
    fn empty() -> Self {
        String::new()
    }
    fn combine(&mut self, other: Self) {
        self.push_str(&other);
    }
}

/// Additive monoid wrapper for numeric types.
#[derive(Debug, Clone, PartialEq)]
pub struct Sum<T>(pub T);

impl Monoid for Sum<i64> {
    fn empty() -> Self {
        Sum(0)
    }
    fn combine(&mut self, other: Self) {
        self.0 += other.0;
    }
}

impl Monoid for Sum<f64> {
    fn empty() -> Self {
        Sum(0.0)
    }
    fn combine(&mut self, other: Self) {
        self.0 += other.0;
    }
}
```

### Source: `src/identity.rs`

```rust
/// The identity monad: wraps a value with no additional effect.
/// Serves as the base of any transformer stack.
#[derive(Debug, Clone, PartialEq)]
pub struct Identity<T>(pub T);

impl<T> Identity<T> {
    pub fn run(self) -> T {
        self.0
    }

    pub fn map<U>(self, f: impl FnOnce(T) -> U) -> Identity<U> {
        Identity(f(self.0))
    }

    pub fn bind<U>(self, f: impl FnOnce(T) -> Identity<U>) -> Identity<U> {
        f(self.0)
    }

    pub fn unit(value: T) -> Self {
        Identity(value)
    }
}
```

### Source: `src/maybe.rs`

```rust
/// Custom Option monad.
#[derive(Debug, Clone, PartialEq)]
pub enum Maybe<T> {
    Just(T),
    Nothing,
}

impl<T> Maybe<T> {
    pub fn unit(value: T) -> Self {
        Maybe::Just(value)
    }

    pub fn map<U>(self, f: impl FnOnce(T) -> U) -> Maybe<U> {
        match self {
            Maybe::Just(v) => Maybe::Just(f(v)),
            Maybe::Nothing => Maybe::Nothing,
        }
    }

    pub fn bind<U>(self, f: impl FnOnce(T) -> Maybe<U>) -> Maybe<U> {
        match self {
            Maybe::Just(v) => f(v),
            Maybe::Nothing => Maybe::Nothing,
        }
    }

    pub fn is_just(&self) -> bool {
        matches!(self, Maybe::Just(_))
    }

    pub fn unwrap_or(self, default: T) -> T {
        match self {
            Maybe::Just(v) => v,
            Maybe::Nothing => default,
        }
    }
}
```

### Source: `src/outcome.rs`

```rust
/// Custom Result monad.
#[derive(Debug, Clone, PartialEq)]
pub enum Outcome<T, E> {
    Success(T),
    Failure(E),
}

impl<T, E> Outcome<T, E> {
    pub fn unit(value: T) -> Self {
        Outcome::Success(value)
    }

    pub fn fail(err: E) -> Self {
        Outcome::Failure(err)
    }

    pub fn map<U>(self, f: impl FnOnce(T) -> U) -> Outcome<U, E> {
        match self {
            Outcome::Success(v) => Outcome::Success(f(v)),
            Outcome::Failure(e) => Outcome::Failure(e),
        }
    }

    pub fn map_err<F>(self, f: impl FnOnce(E) -> F) -> Outcome<T, F> {
        match self {
            Outcome::Success(v) => Outcome::Success(v),
            Outcome::Failure(e) => Outcome::Failure(f(e)),
        }
    }

    pub fn bind<U>(self, f: impl FnOnce(T) -> Outcome<U, E>) -> Outcome<U, E> {
        match self {
            Outcome::Success(v) => f(v),
            Outcome::Failure(e) => Outcome::Failure(e),
        }
    }

    pub fn is_success(&self) -> bool {
        matches!(self, Outcome::Success(_))
    }

    pub fn unwrap_or(self, default: T) -> T {
        match self {
            Outcome::Success(v) => v,
            Outcome::Failure(_) => default,
        }
    }
}
```

### Source: `src/reader.rs`

```rust
/// ReaderT monad transformer: provides read-only access to an environment.
///
/// Conceptually: `ReaderT<Env, Base, A>` = `Fn(&Env) -> Base<A>`
///
/// The environment is passed by reference to avoid cloning large config objects.
pub struct ReaderT<Env, A> {
    run_fn: Box<dyn FnOnce(&Env) -> A>,
}

impl<Env: 'static, A: 'static> ReaderT<Env, A> {
    pub fn new(f: impl FnOnce(&Env) -> A + 'static) -> Self {
        Self { run_fn: Box::new(f) }
    }

    /// Inject a pure value, ignoring the environment.
    pub fn unit(value: A) -> Self
    where
        A: Clone,
    {
        Self::new(move |_| value.clone())
    }

    /// Access the environment.
    pub fn ask() -> ReaderT<Env, Env>
    where
        Env: Clone,
    {
        ReaderT::new(|env: &Env| env.clone())
    }

    /// Transform the result.
    pub fn map<B: 'static>(self, f: impl FnOnce(A) -> B + 'static) -> ReaderT<Env, B> {
        ReaderT::new(move |env| {
            let a = (self.run_fn)(env);
            f(a)
        })
    }

    /// Chain with a function that returns another ReaderT.
    pub fn bind<B: 'static>(
        self,
        f: impl FnOnce(A) -> ReaderT<Env, B> + 'static,
    ) -> ReaderT<Env, B> {
        ReaderT::new(move |env| {
            let a = (self.run_fn)(env);
            let next = f(a);
            (next.run_fn)(env)
        })
    }

    /// Run with a modified environment.
    pub fn local<F>(self, modify_env: F) -> ReaderT<Env, A>
    where
        F: FnOnce(&Env) -> Env + 'static,
        Env: 'static,
    {
        ReaderT::new(move |env| {
            let modified = modify_env(env);
            (self.run_fn)(&modified)
        })
    }

    /// Execute the computation with the given environment.
    pub fn run(self, env: &Env) -> A {
        (self.run_fn)(env)
    }
}
```

### Source: `src/writer.rs`

```rust
use crate::traits::Monoid;

/// WriterT monad transformer: accumulates a monoidal log alongside computation.
///
/// Conceptually: `WriterT<W, A>` = `(A, W)` where W is a Monoid.
#[derive(Debug, Clone)]
pub struct WriterT<W, A> {
    pub value: A,
    pub log: W,
}

impl<W: Monoid + 'static, A: 'static> WriterT<W, A> {
    pub fn new(value: A, log: W) -> Self {
        Self { value, log }
    }

    /// Inject a pure value with an empty log.
    pub fn unit(value: A) -> Self {
        Self {
            value,
            log: W::empty(),
        }
    }

    /// Append to the log without producing a meaningful value.
    pub fn tell(log: W) -> WriterT<W, ()> {
        WriterT { value: (), log }
    }

    /// Transform the value, keeping the log.
    pub fn map<B: 'static>(self, f: impl FnOnce(A) -> B) -> WriterT<W, B> {
        WriterT {
            value: f(self.value),
            log: self.log,
        }
    }

    /// Chain with a function that returns another WriterT.
    /// Logs are combined using Monoid::combine.
    pub fn bind<B: 'static>(self, f: impl FnOnce(A) -> WriterT<W, B>) -> WriterT<W, B> {
        let next = f(self.value);
        let mut combined_log = self.log;
        combined_log.combine(next.log);
        WriterT {
            value: next.value,
            log: combined_log,
        }
    }

    /// Capture the log alongside the value.
    pub fn listen(self) -> WriterT<W, (A, W)>
    where
        W: Clone,
    {
        let log_copy = self.log.clone();
        WriterT {
            value: (self.value, log_copy),
            log: self.log,
        }
    }

    /// Extract value and log.
    pub fn run(self) -> (A, W) {
        (self.value, self.log)
    }
}
```

### Source: `src/state.rs`

```rust
/// StateT monad transformer: threads mutable state through a computation.
///
/// Conceptually: `StateT<S, A>` = `Fn(S) -> (A, S)`
pub struct StateT<S, A> {
    run_fn: Box<dyn FnOnce(S) -> (A, S)>,
}

impl<S: 'static, A: 'static> StateT<S, A> {
    pub fn new(f: impl FnOnce(S) -> (A, S) + 'static) -> Self {
        Self { run_fn: Box::new(f) }
    }

    /// Inject a pure value, passing state through unchanged.
    pub fn unit(value: A) -> Self
    where
        A: Clone,
    {
        Self::new(move |s| (value.clone(), s))
    }

    /// Read the current state.
    pub fn get() -> StateT<S, S>
    where
        S: Clone,
    {
        StateT::new(|s: S| {
            let cloned = s.clone();
            (cloned, s)
        })
    }

    /// Replace the state entirely.
    pub fn put(new_state: S) -> StateT<S, ()>
    where
        S: 'static,
    {
        StateT::new(move |_| ((), new_state))
    }

    /// Modify the state with a function.
    pub fn modify(f: impl FnOnce(S) -> S + 'static) -> StateT<S, ()> {
        StateT::new(move |s| ((), f(s)))
    }

    /// Transform the value.
    pub fn map<B: 'static>(self, f: impl FnOnce(A) -> B + 'static) -> StateT<S, B> {
        StateT::new(move |s| {
            let (a, s2) = (self.run_fn)(s);
            (f(a), s2)
        })
    }

    /// Chain with a function that returns another StateT.
    pub fn bind<B: 'static>(
        self,
        f: impl FnOnce(A) -> StateT<S, B> + 'static,
    ) -> StateT<S, B> {
        StateT::new(move |s| {
            let (a, s2) = (self.run_fn)(s);
            let next = f(a);
            (next.run_fn)(s2)
        })
    }

    /// Execute the stateful computation with an initial state.
    pub fn run(self, initial: S) -> (A, S) {
        (self.run_fn)(initial)
    }

    /// Execute and return only the value.
    pub fn eval(self, initial: S) -> A {
        (self.run_fn)(initial).0
    }

    /// Execute and return only the final state.
    pub fn exec(self, initial: S) -> S {
        (self.run_fn)(initial).1
    }
}
```

### Source: `src/stack.rs`

```rust
use crate::outcome::Outcome;
use crate::traits::Monoid;

/// A composed monad stack: Reader + Writer + Result.
///
/// `AppStack<Env, W, T, E>` represents a computation that:
/// - Reads from an environment of type Env
/// - Accumulates a log of type W (must be Monoid)
/// - May fail with error type E
/// - Produces a value of type T on success
///
/// Conceptually: `Fn(&Env) -> (Outcome<T, E>, W)`
pub struct AppStack<Env, W, T, E> {
    run_fn: Box<dyn FnOnce(&Env) -> (Outcome<T, E>, W)>,
}

impl<Env: 'static, W: Monoid + 'static, T: 'static, E: 'static> AppStack<Env, W, T, E> {
    pub fn new(f: impl FnOnce(&Env) -> (Outcome<T, E>, W) + 'static) -> Self {
        Self { run_fn: Box::new(f) }
    }

    /// Inject a pure value.
    pub fn unit(value: T) -> Self
    where
        T: Clone,
    {
        Self::new(move |_| (Outcome::Success(value.clone()), W::empty()))
    }

    /// Fail with an error.
    pub fn fail(err: E) -> Self
    where
        E: Clone,
    {
        Self::new(move |_| (Outcome::Failure(err.clone()), W::empty()))
    }

    /// Access the environment.
    pub fn ask() -> AppStack<Env, W, Env, E>
    where
        Env: Clone,
    {
        AppStack::new(move |env: &Env| (Outcome::Success(env.clone()), W::empty()))
    }

    /// Append to the log.
    pub fn tell(log: W) -> AppStack<Env, W, (), E> {
        AppStack::new(move |_| (Outcome::Success(()), log))
    }

    /// Transform the success value.
    pub fn map<U: 'static>(self, f: impl FnOnce(T) -> U + 'static) -> AppStack<Env, W, U, E> {
        AppStack::new(move |env| {
            let (outcome, log) = (self.run_fn)(env);
            (outcome.map(f), log)
        })
    }

    /// Chain with a fallible computation. Short-circuits on failure, accumulates logs.
    pub fn bind<U: 'static>(
        self,
        f: impl FnOnce(T) -> AppStack<Env, W, U, E> + 'static,
    ) -> AppStack<Env, W, U, E> {
        AppStack::new(move |env| {
            let (outcome, mut log1) = (self.run_fn)(env);
            match outcome {
                Outcome::Success(val) => {
                    let next = f(val);
                    let (result, log2) = (next.run_fn)(env);
                    log1.combine(log2);
                    (result, log1)
                }
                Outcome::Failure(e) => (Outcome::Failure(e), log1),
            }
        })
    }

    /// Execute the full stack.
    pub fn run(self, env: &Env) -> (Outcome<T, E>, W) {
        (self.run_fn)(env)
    }
}
```

### Source: `src/lib.rs`

```rust
pub mod traits;
pub mod identity;
pub mod maybe;
pub mod outcome;
pub mod reader;
pub mod writer;
pub mod state;
pub mod stack;
```

### Source: `src/main.rs`

```rust
use monad_transformers::identity::Identity;
use monad_transformers::maybe::Maybe;
use monad_transformers::outcome::Outcome;
use monad_transformers::reader::ReaderT;
use monad_transformers::writer::WriterT;
use monad_transformers::state::StateT;
use monad_transformers::stack::AppStack;

fn main() {
    println!("=== Identity Monad ===\n");

    let result = Identity::unit(10)
        .bind(|x| Identity::unit(x * 2))
        .bind(|x| Identity::unit(x + 3))
        .run();
    println!("Identity: 10 -> *2 -> +3 = {result}");

    println!("\n=== Maybe Monad ===\n");

    let result = Maybe::unit(42)
        .bind(|x| if x > 0 { Maybe::Just(x * 2) } else { Maybe::Nothing })
        .bind(|x| Maybe::Just(x + 10));
    println!("Maybe: 42 -> (>0 ? *2) -> +10 = {:?}", result);

    let nothing: Maybe<i32> = Maybe::unit(0)
        .bind(|x| if x > 0 { Maybe::Just(x) } else { Maybe::Nothing })
        .bind(|x| Maybe::Just(x + 10));
    println!("Maybe: 0 -> (>0 ? pass) -> +10 = {:?}", nothing);

    println!("\n=== Outcome Monad ===\n");

    let ok: Outcome<i32, String> = Outcome::unit(5)
        .bind(|x| if x != 0 { Outcome::Success(100 / x) } else { Outcome::fail("div by zero".into()) });
    println!("Outcome: 100/5 = {:?}", ok);

    let err: Outcome<i32, String> = Outcome::unit(0)
        .bind(|x| if x != 0 { Outcome::Success(100 / x) } else { Outcome::fail("div by zero".into()) });
    println!("Outcome: 100/0 = {:?}", err);

    println!("\n=== ReaderT ===\n");

    #[derive(Debug, Clone)]
    struct Config {
        db_url: String,
        max_retries: u32,
    }

    let config = Config {
        db_url: "postgres://localhost/mydb".into(),
        max_retries: 3,
    };

    let computation = ReaderT::<Config, String>::ask()
        .map(|cfg| format!("Connecting to {} (max {} retries)", cfg.db_url, cfg.max_retries));

    let result = computation.run(&config);
    println!("Reader result: {result}");

    println!("\n=== WriterT ===\n");

    let computation = WriterT::<Vec<String>, i32>::unit(10)
        .bind(|x| {
            let mut w = WriterT::tell(vec!["starting computation".into()]);
            w = w.bind(|_| WriterT::new(x * 2, vec!["doubled the value".into()]));
            w
        })
        .bind(|x| WriterT::new(x + 5, vec![format!("added 5, result is {}", x + 5)]));

    let (value, log) = computation.run();
    println!("Writer value: {value}");
    println!("Writer log:");
    for entry in &log {
        println!("  - {entry}");
    }

    println!("\n=== StateT ===\n");

    let computation = StateT::<Vec<String>, ()>::modify(|mut stack: Vec<String>| {
        stack.push("first".into());
        stack
    })
    .bind(|_| StateT::modify(|mut stack: Vec<String>| {
        stack.push("second".into());
        stack
    }))
    .bind(|_| StateT::modify(|mut stack: Vec<String>| {
        stack.push("third".into());
        stack
    }))
    .bind(|_| StateT::<Vec<String>, Vec<String>>::get());

    let (final_stack, _) = computation.run(Vec::new());
    println!("State result: {:?}", final_stack);

    println!("\n=== Composed AppStack: User Registration ===\n");

    #[derive(Debug, Clone)]
    struct AppConfig {
        min_name_length: usize,
        min_password_length: usize,
        allowed_domains: Vec<String>,
    }

    #[derive(Debug, Clone)]
    struct AppError(String);

    let app_config = AppConfig {
        min_name_length: 2,
        min_password_length: 8,
        allowed_domains: vec!["example.com".into(), "test.org".into()],
    };

    let register_user = |name: String, email: String, password: String| {
        AppStack::<AppConfig, Vec<String>, (), AppError>::tell(
            vec![format!("Starting registration for {name}")]
        )
        .bind(move |_| {
            AppStack::ask().bind(move |config: AppConfig| {
                if name.len() < config.min_name_length {
                    return AppStack::fail(AppError(format!(
                        "Name must be at least {} chars", config.min_name_length
                    )));
                }
                AppStack::<AppConfig, Vec<String>, String, AppError>::tell(
                    vec!["Name validated".into()]
                ).map(move |_| name)
            })
        })
        .bind(move |validated_name| {
            AppStack::ask().bind(move |config: AppConfig| {
                let domain = email.split('@').last().unwrap_or("");
                if !config.allowed_domains.iter().any(|d| d == domain) {
                    return AppStack::fail(AppError(format!(
                        "Domain {domain} not allowed"
                    )));
                }
                AppStack::<AppConfig, Vec<String>, (String, String), AppError>::tell(
                    vec!["Email validated".into()]
                ).map(move |_| (validated_name, email))
            })
        })
        .bind(move |(name, email)| {
            AppStack::ask().bind(move |config: AppConfig| {
                if password.len() < config.min_password_length {
                    return AppStack::fail(AppError(format!(
                        "Password must be at least {} chars", config.min_password_length
                    )));
                }
                AppStack::<AppConfig, Vec<String>, String, AppError>::tell(
                    vec![format!("User {} <{}> registered successfully", name, email)]
                ).map(move |_| format!("User({name}, {email})"))
            })
        })
    };

    // Successful registration.
    let (result, log) = register_user(
        "Alice".into(),
        "alice@example.com".into(),
        "securepassword".into(),
    ).run(&app_config);
    println!("Result: {:?}", result);
    println!("Log:");
    for entry in &log {
        println!("  - {entry}");
    }

    println!();

    // Failed registration.
    let (result, log) = register_user(
        "A".into(),
        "a@bad-domain.com".into(),
        "short".into(),
    ).run(&app_config);
    println!("Result: {:?}", result);
    println!("Log:");
    for entry in &log {
        println!("  - {entry}");
    }
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use monad_transformers::identity::Identity;
    use monad_transformers::maybe::Maybe;
    use monad_transformers::outcome::Outcome;
    use monad_transformers::reader::ReaderT;
    use monad_transformers::writer::WriterT;
    use monad_transformers::state::StateT;
    use monad_transformers::stack::AppStack;
    use monad_transformers::traits::{Monoid, Sum};

    // === Monad Laws ===

    // Left identity: unit(a).bind(f) == f(a)
    // Right identity: m.bind(unit) == m
    // Associativity: m.bind(f).bind(g) == m.bind(|x| f(x).bind(g))

    #[test]
    fn identity_left_identity() {
        let f = |x: i32| Identity(x * 2);
        let left = Identity::unit(10).bind(f);
        let right = f(10);
        assert_eq!(left, right);
    }

    #[test]
    fn identity_right_identity() {
        let m = Identity(42);
        let result = m.clone().bind(Identity::unit);
        assert_eq!(m, result);
    }

    #[test]
    fn identity_associativity() {
        let f = |x: i32| Identity(x + 1);
        let g = |x: i32| Identity(x * 3);
        let m = Identity(5);

        let left = m.clone().bind(f).bind(g);
        let right = m.bind(|x| f(x).bind(g));
        assert_eq!(left, right);
    }

    #[test]
    fn maybe_left_identity() {
        let f = |x: i32| Maybe::Just(x * 2);
        let left = Maybe::unit(10).bind(f);
        let right = f(10);
        assert_eq!(left, right);
    }

    #[test]
    fn maybe_right_identity() {
        let m = Maybe::Just(42);
        let result = m.clone().bind(Maybe::unit);
        assert_eq!(m, result);
    }

    #[test]
    fn maybe_associativity() {
        let f = |x: i32| Maybe::Just(x + 1);
        let g = |x: i32| Maybe::Just(x * 3);
        let m = Maybe::Just(5);

        let left = m.clone().bind(f).bind(g);
        let right = m.bind(|x| f(x).bind(g));
        assert_eq!(left, right);
    }

    #[test]
    fn maybe_nothing_propagates() {
        let result: Maybe<i32> = Maybe::Nothing
            .bind(|x: i32| Maybe::Just(x + 1))
            .bind(|x| Maybe::Just(x * 2));
        assert_eq!(result, Maybe::Nothing);
    }

    #[test]
    fn outcome_left_identity() {
        let f = |x: i32| Outcome::<_, String>::Success(x * 2);
        let left = Outcome::<_, String>::unit(10).bind(f);
        let right = f(10);
        assert_eq!(left, right);
    }

    #[test]
    fn outcome_right_identity() {
        let m: Outcome<i32, String> = Outcome::Success(42);
        let result = m.clone().bind(Outcome::unit);
        assert_eq!(m, result);
    }

    #[test]
    fn outcome_failure_propagates() {
        let result: Outcome<i32, String> = Outcome::fail("err".into())
            .bind(|x: i32| Outcome::Success(x + 1));
        assert_eq!(result, Outcome::Failure("err".into()));
    }

    // === Monoid Laws ===

    #[test]
    fn monoid_vec_identity() {
        let mut v = vec![1, 2, 3];
        let empty: Vec<i32> = Vec::empty();
        v.combine(empty);
        assert_eq!(v, vec![1, 2, 3]);
    }

    #[test]
    fn monoid_vec_combine() {
        let mut a = vec![1, 2];
        a.combine(vec![3, 4]);
        assert_eq!(a, vec![1, 2, 3, 4]);
    }

    #[test]
    fn monoid_string_combine() {
        let mut s = "hello".to_string();
        s.combine(" world".to_string());
        assert_eq!(s, "hello world");
    }

    #[test]
    fn monoid_sum_combine() {
        let mut a = Sum(10i64);
        a.combine(Sum(20));
        assert_eq!(a, Sum(30));
    }

    // === ReaderT tests ===

    #[test]
    fn reader_ask() {
        let comp = ReaderT::<i32, i32>::ask().map(|x| x * 2);
        assert_eq!(comp.run(&21), 42);
    }

    #[test]
    fn reader_bind() {
        let comp = ReaderT::<String, String>::ask()
            .bind(|env: String| ReaderT::new(move |_: &String| env.len()));
        assert_eq!(comp.run(&"hello".to_string()), 5);
    }

    #[test]
    fn reader_local() {
        let comp = ReaderT::<i32, i32>::ask().local(|x: &i32| x + 10);
        assert_eq!(comp.run(&5), 15);
    }

    // === WriterT tests ===

    #[test]
    fn writer_tell_and_run() {
        let comp = WriterT::<Vec<String>, ()>::tell(vec!["hello".into()])
            .bind(|_| WriterT::tell(vec!["world".into()]));
        let ((), log) = comp.run();
        assert_eq!(log, vec!["hello", "world"]);
    }

    #[test]
    fn writer_value_and_log() {
        let comp = WriterT::new(42, vec!["computed".to_string()]);
        let (val, log) = comp.run();
        assert_eq!(val, 42);
        assert_eq!(log, vec!["computed"]);
    }

    #[test]
    fn writer_bind_accumulates_log() {
        let comp = WriterT::new(10, vec!["start".to_string()])
            .bind(|x| WriterT::new(x * 2, vec!["doubled".to_string()]))
            .bind(|x| WriterT::new(x + 1, vec!["incremented".to_string()]));

        let (val, log) = comp.run();
        assert_eq!(val, 21);
        assert_eq!(log, vec!["start", "doubled", "incremented"]);
    }

    #[test]
    fn writer_listen() {
        let comp = WriterT::new(42, vec!["msg".to_string()]).listen();
        let ((val, captured_log), log) = comp.run();
        assert_eq!(val, 42);
        assert_eq!(captured_log, vec!["msg"]);
        assert_eq!(log, vec!["msg"]);
    }

    // === StateT tests ===

    #[test]
    fn state_get_put() {
        let comp = StateT::<i32, i32>::get()
            .bind(|x| StateT::put(x * 2).bind(|_| StateT::get()));
        let (val, state) = comp.run(21);
        assert_eq!(val, 42);
        assert_eq!(state, 42);
    }

    #[test]
    fn state_modify() {
        let comp = StateT::<Vec<i32>, ()>::modify(|mut v: Vec<i32>| { v.push(1); v })
            .bind(|_| StateT::modify(|mut v: Vec<i32>| { v.push(2); v }))
            .bind(|_| StateT::modify(|mut v: Vec<i32>| { v.push(3); v }))
            .bind(|_| StateT::<Vec<i32>, Vec<i32>>::get());

        let (val, state) = comp.run(vec![]);
        assert_eq!(val, vec![1, 2, 3]);
        assert_eq!(state, vec![1, 2, 3]);
    }

    #[test]
    fn state_eval_exec() {
        let comp = StateT::new(|s: i32| (s * 2, s + 1));
        let value = StateT::new(|s: i32| (s * 2, s + 1)).eval(10);
        assert_eq!(value, 20);

        let state = comp.exec(10);
        assert_eq!(state, 11);
    }

    // === AppStack tests ===

    #[test]
    fn app_stack_success() {
        let comp = AppStack::<i32, Vec<String>, i32, String>::ask()
            .bind(|env| {
                AppStack::tell(vec![format!("got env: {env}")])
                    .map(move |_| env * 2)
            });

        let (result, log) = comp.run(&21);
        assert_eq!(result, Outcome::Success(42));
        assert_eq!(log, vec!["got env: 21"]);
    }

    #[test]
    fn app_stack_failure_short_circuits() {
        let comp: AppStack<(), Vec<String>, i32, String> =
            AppStack::tell(vec!["before failure".into()])
                .bind(|_| AppStack::<(), Vec<String>, i32, String>::fail("boom".into()))
                .bind(|x| {
                    AppStack::tell(vec!["should not appear".into()])
                        .map(move |_| x + 1)
                });

        let (result, log) = comp.run(&());
        assert!(matches!(result, Outcome::Failure(ref e) if e == "boom"));
        assert_eq!(log, vec!["before failure"]);
        assert!(!log.contains(&"should not appear".to_string()));
    }

    #[test]
    fn app_stack_logs_accumulate_on_success() {
        let comp = AppStack::<(), Vec<String>, i32, String>::tell(vec!["step 1".into()])
            .bind(|_| AppStack::tell(vec!["step 2".into()]).map(|_| 42))
            .bind(|x| AppStack::tell(vec!["step 3".into()]).map(move |_| x));

        let (result, log) = comp.run(&());
        assert_eq!(result, Outcome::Success(42));
        assert_eq!(log, vec!["step 1", "step 2", "step 3"]);
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
=== Identity Monad ===

Identity: 10 -> *2 -> +3 = 23

=== Maybe Monad ===

Maybe: 42 -> (>0 ? *2) -> +10 = Just(94)
Maybe: 0 -> (>0 ? pass) -> +10 = Nothing

=== Outcome Monad ===

Outcome: 100/5 = Success(20)
Outcome: 100/0 = Failure("div by zero")

=== ReaderT ===

Reader result: Connecting to postgres://localhost/mydb (max 3 retries)

=== WriterT ===

Writer value: 25
Writer log:
  - starting computation
  - doubled the value
  - added 5, result is 25

=== StateT ===

State result: ["first", "second", "third"]

=== Composed AppStack: User Registration ===

Result: Success("User(Alice, alice@example.com)")
Log:
  - Starting registration for Alice
  - Name validated
  - Email validated
  - User Alice <alice@example.com> registered successfully

Result: Failure(AppError("Name must be at least 2 chars"))
Log:
  - Starting registration for A
```

## Design Decisions

1. **Boxed closures for transformers**: `ReaderT` and `StateT` store `Box<dyn FnOnce(...)>` because Rust closure types are unnameable. Each `bind` creates a new closure wrapping the previous one, requiring a heap allocation. This is the standard trade-off in Rust: nominal types for data, boxed closures for deferred computation. The alternative (enum-based free monad encoding) is more complex and provides worse ergonomics.

2. **WriterT as a struct, not a closure**: Unlike ReaderT and StateT, WriterT does not defer computation. It eagerly holds `(value, log)`. This avoids boxing and makes it the most efficient transformer. The downside is that it cannot lazily accumulate -- the log is built immediately during bind.

3. **AppStack as a concrete composed type**: Rather than mechanically stacking `ReaderT<Env, WriterT<W, Outcome<T, E>>>` (which creates deeply nested types and complex bind implementations), `AppStack` is a purpose-built three-layer composition. This sacrifices generality for usability. A production library would use the mechanical approach; for learning, the explicit version is clearer.

4. **Environment passed by reference**: `ReaderT` passes `&Env` rather than `Env` to avoid cloning large configuration objects on every bind. The closure captures `&Env` by reborrowing. This requires the environment to outlive the computation, which is natural for config objects.

5. **No HKT trait unification**: A "pure" implementation would define `trait Monad { type F<A>; fn bind<A, B>(fa: F<A>, f: Fn(A) -> F<B>) -> F<B>; }` and implement it once for each transformer. Rust's type system makes this extremely verbose and error-prone. Instead, each type gets its own `unit`, `bind`, and `map` methods. This duplicates the interface but keeps each implementation clear and self-contained.

## Common Mistakes

1. **Moving the environment in ReaderT bind**: The environment must be available to both the first computation and the continuation. If you move it into the first closure, the second closure cannot access it. The solution is to pass it by reference (`&Env`) so both closures can borrow it.

2. **Forgetting to combine logs in WriterT bind**: The bind operation must combine the log from the first computation with the log from the second. A common bug is to keep only the second log, discarding the accumulated history.

3. **Wrong transformer ordering**: `ReaderT<E, WriterT<W, Outcome<T, Err>>>` means errors discard subsequent log entries but preserve prior ones. `ReaderT<E, Outcome<WriterT<W, T>, Err>>` means an error discards the entire log. The ordering determines error-recovery semantics.

4. **Lifetime issues with nested closures**: Each `bind` wraps the previous closure in a new one. Without boxing, this creates deeply nested closure types that hit Rust's type inference limits. The symptom is "overflow evaluating type requirements" compiler errors.

## Performance Notes

| Operation | Cost | Notes |
|-----------|------|-------|
| `Identity::bind` | 0 allocations | Inlined away entirely |
| `Maybe::bind` | 0 allocations | Pattern match + closure call |
| `Outcome::bind` | 0 allocations | Pattern match + closure call |
| `ReaderT::bind` | 1 Box allocation | Closure wrapping |
| `WriterT::bind` | 0 allocations + log combine | Vec/String append cost |
| `StateT::bind` | 1 Box allocation | Closure wrapping |
| `AppStack::bind` | 1 Box allocation + log combine | Combined Reader + Writer cost |

For a pipeline of N `bind` steps, `ReaderT` and `StateT` allocate O(N) boxes. These allocations are sequential (each wraps the previous), so the total memory at any point is O(N). At execution time (`run`), the closures unwind in order, each consuming its box. The base monads (`Identity`, `Maybe`, `Outcome`) have zero allocation overhead and compile down to the same code as hand-written pattern matching.

## Going Further

- Implement `ExceptT<E, Base>` as a proper error transformer separate from `Outcome`
- Add `ContT<R, Base>` (continuation transformer) for delimited continuation support
- Build a `lift` mechanism that lets inner monads be used within outer transformer layers
- Implement `MonadIO` for a transformer stack with `Identity` at the base, adding actual I/O capability
- Profile the Box allocation overhead and experiment with arena allocation for closure storage
- Explore the `frunk` or `higher` crates' approaches to HKT encoding in Rust
