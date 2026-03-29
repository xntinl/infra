<!-- difficulty: advanced -->
<!-- category: functional-programming -->
<!-- languages: [rust] -->
<!-- concepts: [monad-transformers, higher-kinded-types, reader-monad, writer-monad, state-monad, trait-composition] -->
<!-- estimated_time: 14-20 hours -->
<!-- bloom_level: apply, analyze, evaluate, create -->
<!-- prerequisites: [rust-generics, trait-system-advanced, closures-fn-traits, associated-types, gats] -->

# Challenge 116: Monad Transformer Stack

## Languages

Rust (stable, latest edition)

## Prerequisites

- Deep understanding of Rust generics, associated types, and Generic Associated Types (GATs)
- Familiarity with `Fn`, `FnOnce`, `FnMut` trait bounds and closure composition
- Experience implementing complex traits with multiple associated types
- Conceptual understanding of monads (bind/return/map) from functional programming

## Learning Objectives

- **Implement** fundamental monads (Identity, Option, Result) as trait-based types in Rust
- **Apply** monad transformer patterns to compose effects without nested types
- **Design** ReaderT, WriterT, and StateT transformers that stack cleanly on any base monad
- **Analyze** how transformer ordering affects behavior and ergonomics
- **Evaluate** the trade-offs of simulating higher-kinded types in Rust's type system
- **Create** a practical application that composes config-reading, logging, and error handling into a single monadic stack

## The Challenge

Build a monad transformer library in Rust. Monad transformers solve a fundamental composition problem: you have a computation that needs to read configuration (Reader), accumulate logs (Writer), and handle errors (Result) -- but nesting `Reader<Writer<Result<T>>>` manually is deeply unpleasant. Monad transformers let you stack these effects cleanly.

The challenge is that Rust does not have higher-kinded types (HKTs). You cannot write `trait Monad<F: * -> *>` like in Haskell. Instead, you will simulate HKTs using Generic Associated Types (GATs) and careful trait design. This is the core intellectual puzzle: encoding a concept that Rust's type system does not natively support.

Start with the base monads: `Identity<T>` (does nothing, serves as the base), `Maybe<T>` (optional values), and `Outcome<T, E>` (fallible values). Then build transformers on top: `ReaderT<Env, M>` adds environment access to any monad M, `WriterT<W, M>` adds log accumulation, and `StateT<S, M>` adds mutable state threading. Finally, compose them into a practical stack like `ReaderT<Config, WriterT<Vec<String>, Outcome<T, AppError>>>` and write a real application using it.

## Requirements

1. Define a `Monad` trait using GATs: `type Wrapped<A>`, `fn unit(a: A) -> Self::Wrapped<A>`, `fn bind<B>(self, f) -> Self::Wrapped<B>`
2. Implement `Identity<T>` -- the trivial monad that wraps a value and does nothing else
3. Implement `Maybe<T>` -- optional values with monadic bind (short-circuits on Nothing)
4. Implement `Outcome<T, E>` -- fallible computation with monadic bind (propagates errors)
5. Build `ReaderT<Env, Base>` -- transformer that provides read-only access to an environment value. Implement `ask()` (get the environment), `local()` (run with modified environment), and `run(env)` (execute the computation)
6. Build `WriterT<W, Base>` where `W: Monoid` -- transformer that accumulates a log alongside the computation. Implement `tell(w)` (append to log), `listen()` (capture the log alongside the value), and `run()` (extract value and log)
7. Build `StateT<S, Base>` -- transformer that threads mutable state through a computation. Implement `get()` (read state), `put(s)` (replace state), `modify(f)` (transform state), and `run(initial)` (execute with initial state)
8. Define a `Monoid` trait with `empty()` and `combine(&mut self, other)` and implement it for `Vec<T>`, `String`, and numeric types (under addition)
9. Compose a stack: `ReaderT<Config, WriterT<Vec<String>, Outcome<T, AppError>>>` that reads config, logs operations, and handles errors
10. Build a practical example: a user registration pipeline that reads DB config, logs each step, validates input, and returns a result or accumulated errors
11. Ensure monad laws hold: left identity, right identity, and associativity
12. Write property-style tests verifying the monad laws for each transformer

## Hints

Hints for advanced challenges are intentionally sparse. These point you in the right direction without revealing the implementation.

- The GAT trick: `trait Monad { type Wrapped<A>; }` lets you express "same container, different inner type." For `Maybe`, `Wrapped<A>` is `Maybe<A>`. For `Outcome<T, E>`, it fixes `E` and varies `T`.
- `ReaderT<Env, Base>` is essentially `Fn(Env) -> Base::Wrapped<A>`. The transformer wraps a function from environment to base-monad computation. `bind` threads the environment through both the original computation and the continuation.
- `WriterT<W, Base>` wraps `Base::Wrapped<(A, W)>` -- every computation produces a value and a log. `bind` runs the first computation, then the second, then combines logs using `Monoid::combine`.
- `StateT<S, Base>` wraps `Fn(S) -> Base::Wrapped<(A, S)>` -- a function from state to (value, new-state) inside the base monad. This is the most complex transformer because the state threads sequentially.
- Transformer order matters: `ReaderT<E, WriterT<W, Outcome<T, Err>>>` means "the Reader has access to the environment, then Writer accumulates logs, then the bottom layer handles errors." If you swap Writer and Outcome, errors discard the log.
- In Rust, you will likely need to box closures (`Box<dyn FnOnce(...)>`) to store transformer computations, because closure types are unnameable. This adds a heap allocation per monadic operation.

## Acceptance Criteria

- [ ] `Monad` trait is defined with GATs and implemented for `Identity`, `Maybe`, and `Outcome`
- [ ] `Monoid` trait is defined and implemented for `Vec<T>`, `String`, and at least one numeric type
- [ ] `ReaderT` correctly provides environment access, `ask()`, `local()`, and `run()`
- [ ] `WriterT` correctly accumulates logs via `tell()`, and `run()` returns (value, log)
- [ ] `StateT` correctly threads state through computations with `get()`, `put()`, `modify()`, and `run()`
- [ ] A composed stack of at least three transformers works correctly end-to-end
- [ ] The practical example demonstrates config-reading, logging, and error handling in one pipeline
- [ ] Monad laws (left identity, right identity, associativity) verified in tests for all monads
- [ ] All tests pass with `cargo test`

## Research Resources

- [Monad Transformers Step by Step (Martin Grabmuller)](https://page.mi.fu-berlin.de/scravy/realworldhaskell/materialien/monad-transformers-step-by-step.pdf) -- the clearest tutorial on stacking transformers
- [Higher-Kinded Types in Rust (Will Crichton)](https://willcrichton.net/notes/higher-ranked-polymorphism-in-rust/) -- HKT simulation techniques
- [Rust GATs stabilization RFC](https://rust-lang.github.io/rfcs/1598-generic_associated_types.html) -- understanding GATs
- [Simulating HKTs in Rust (blog)](https://hugopeters.me/posts/14/) -- practical approaches to HKT encoding
- [Haskell MTL library source](https://hackage.haskell.org/package/mtl) -- reference implementations of ReaderT, WriterT, StateT
- [Rust and the Monad trait (without HKTs)](https://varkor.github.io/blog/2019/03/28/idiomatic-monads-in-rust.html) -- challenges and approaches
