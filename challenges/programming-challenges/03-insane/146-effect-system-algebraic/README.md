<!-- difficulty: insane -->
<!-- category: functional-programming, language-design -->
<!-- languages: [rust] -->
<!-- concepts: [algebraic-effects, delimited-continuations, effect-handlers, CPS-transform, dependency-injection] -->
<!-- estimated_time: 30-50 hours -->
<!-- bloom_level: analyze, evaluate, create -->
<!-- prerequisites: [closures-advanced, trait-objects, boxing-dyn-dispatch, continuation-passing-style, monad-basics] -->

# Challenge 146: Algebraic Effect System

## Languages

Rust (stable, latest edition)

## Prerequisites

- Advanced understanding of closures, trait objects, and dynamic dispatch in Rust
- Familiarity with continuation-passing style (CPS) and its relationship to control flow
- Experience with trait-based polymorphism and `Box<dyn Trait>` patterns
- Understanding of monadic effect composition and dependency injection concepts

## Learning Objectives

- **Analyze** how algebraic effects decouple effectful operations from their implementations
- **Evaluate** the trade-offs between algebraic effects, monads, and traditional dependency injection
- **Create** a working effect system with effect declaration, handler installation, and continuation-based dispatch
- **Create** practical examples demonstrating testable I/O, mock injection, and nested handler scoping

## The Challenge

Build an algebraic effect system in Rust. Algebraic effects are a powerful alternative to monads for structuring side effects. Instead of wrapping computations in monadic types, effectful operations are declared as "requests" that bubble up to a "handler" which interprets them. The handler can resume the computation with a value, abort it, or invoke the continuation multiple times.

The key insight: a computation does not perform effects directly. It performs an effect operation (like "read a line from stdin") which suspends the computation and passes a continuation to the handler. The handler decides how to fulfill the request (actually read stdin, or return a mock value for testing) and resumes the computation with the result.

You will build: an effect declaration mechanism (traits representing capabilities like Console, FileSystem, Random), a handler installation mechanism (binding effect implementations to scoped regions of code), and a continuation-based dispatch system that suspends and resumes computations at effect boundaries. Built-in effects should include I/O, mutable state, and exceptions. The system must support nested handlers where inner handlers can delegate to outer ones.

## Requirements

1. Define an `Effect` trait that all effect types implement, with an associated `Output` type
2. Build an `Eff<T>` computation type that represents a computation producing `T` which may perform effects
3. Implement `perform<E: Effect>(effect: E) -> Eff<E::Output>` to invoke an effect and suspend until handled
4. Build a `Handler` mechanism that intercepts specific effect types and provides implementations
5. Support continuation-based resumption: handlers receive a continuation `k: Box<dyn FnOnce(Output) -> Eff<T>>` and can resume, abort, or invoke it multiple times
6. Implement nested handler scoping: inner handlers shadow outer handlers for the same effect type
7. Build a `Console` effect with `ReadLine` and `PrintLine` operations
8. Build a `State<S>` effect with `Get` and `Put(S)` operations
9. Build an `Exception<E>` effect with `Raise(E)` operation (handler can catch or propagate)
10. Build a `Random` effect with `NextInt(range)` and `NextFloat` operations
11. Write a practical example: an interactive program that reads input, maintains state, and handles errors -- fully testable by swapping handlers
12. Demonstrate testing: run the same computation with real Console handler and mock Console handler

## Hints

Insane challenges provide minimal guidance. These are directional signposts only.

- Model `Eff<T>` as an enum: `Pure(T)` for completed computations, `Impure { effect, continuation }` for suspended ones. The continuation transforms the effect's output into the next `Eff<T>`.
- Use `Box<dyn Any>` for type-erased effect storage, then downcast in handlers. This is the pragmatic Rust approach since the effect type is not known statically at the `Eff` level.
- Handlers are functions `Fn(effect, continuation) -> Eff<T>`. A handler that resumes normally just calls `continuation(result)`. A handler that catches exceptions returns `Pure(default_value)` without calling the continuation.
- For nested handlers, run the inner computation and check each `Impure` step: if the effect matches this handler, handle it; otherwise, re-wrap it as `Impure` and propagate outward.

## Acceptance Criteria

- [ ] `Effect` trait with associated `Output` type is defined
- [ ] `Eff<T>` represents pure and suspended computations
- [ ] `perform()` creates a suspended computation waiting for a handler
- [ ] Handlers intercept effects and resume computations via continuations
- [ ] Handlers can resume, abort, or invoke continuations multiple times
- [ ] Nested handlers work: inner handlers shadow outer ones for the same effect
- [ ] `Console`, `State<S>`, `Exception<E>`, and `Random` effects are implemented
- [ ] Practical example runs with real and mock handlers producing correct results
- [ ] All tests pass with `cargo test`

## Research Resources

- [An Introduction to Algebraic Effects and Handlers (Matija Pretnar)](https://www.eff-lang.org/handlers-tutorial.pdf) -- the clearest tutorial on the theory
- [Algebraic Effects for the Rest of Us (Dan Abramov)](https://overreacted.io/algebraic-effects-for-the-rest-of-us/) -- intuitive JavaScript-perspective explanation
- [Eff programming language](https://www.eff-lang.org/) -- a language built around algebraic effects
- [Effect Handlers in Scope (Wu et al., 2014)](https://www.cs.ox.ac.uk/people/nicolas.wu/papers/Scope.pdf) -- scoped handlers and nested semantics
- [Koka language effect system](https://koka-lang.github.io/koka/doc/index.html) -- production language with row-polymorphic effects
- [Implementing Algebraic Effects in C (Leijen, 2017)](https://www.microsoft.com/en-us/research/wp-content/uploads/2017/06/algeff-in-c-tr-v2.pdf) -- low-level implementation strategies
