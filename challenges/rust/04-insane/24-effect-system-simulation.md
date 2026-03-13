# 24. Effect System Simulation

**Difficulty**: Insane

## The Challenge

Design and implement a Rust library that simulates algebraic effects using the trait
system, generics, and (optionally) nightly coroutines. Your library must allow users
to write functions that declare which side effects they perform, define handlers that
intercept and interpret those effects, and compose effectful computations without the
"function coloring" problem that plagues async Rust.

Concretely, build a library where:

- Effects are defined as traits (e.g., `trait Console`, `trait FileSystem`, `trait Random`)
- Effectful functions declare their effects in their signature
- Handlers provide concrete implementations that can vary by callsite
- The same effectful function can be run in production (real I/O), in tests (mocked
  I/O), synchronously, or asynchronously — without changing the function's source code

Then use your library to implement a non-trivial program: a **configuration loader**
that reads files, parses TOML, logs warnings, and returns structured config — where
every side effect (file I/O, logging, error handling) is an algebraic effect with
swappable handlers.

This matters because algebraic effects solve the expression problem for side effects.
Monads require transformer stacks that do not compose well. Rust's current approach
(async is one built-in effect, `?` is another, but they do not generalize) leads to
ecosystem splits. Languages like Koka and OCaml 5 demonstrate that algebraic effects
can unify async, exceptions, state, and I/O under a single mechanism. Understanding
how to approximate this in Rust reveals both the power and the limitations of the
type system.

## Acceptance Criteria

- [ ] Define at least three effect traits: `Console` (read/write), `FileSystem` (read file, write file), and `Random` (generate random number)
- [ ] Effectful functions declare their effects as trait bounds: `fn load_config<E: FileSystem + Console>(path: &str) -> Result<Config, Error>` or an equivalent encoding
- [ ] Implement at least two handlers per effect: one "real" handler (actual I/O) and one "test" handler (deterministic, in-memory)
- [ ] Effects compose: a function requiring `FileSystem + Console` can call subfunctions requiring only `FileSystem`
- [ ] The `?` operator works within effectful computations for error propagation (Result as an effect)
- [ ] Demonstrate handler swapping: the same `load_config` function runs against the real filesystem in production and an in-memory filesystem in tests, with identical code
- [ ] No runtime overhead for the handler dispatch in release mode (the compiler monomorphizes away the handler indirection)
- [ ] Implement effect transformation: a handler that translates one effect into another (e.g., `FileSystem` implemented in terms of `Console` for a REPL that asks the user for file contents)
- [ ] Write at least 10 unit tests using the test handlers that verify effectful logic without any real I/O
- [ ] Document the limitations: what Rust cannot express (higher-kinded types, true delimited continuations) and how your design works around them
- [ ] If using nightly: demonstrate the coroutine-based approach where effectful functions yield effect requests and handlers resume them with responses

## Background

An algebraic effect is a structured side effect. Instead of a function directly
performing I/O, it *requests* an effect (e.g., "read this file") and *yields*
control to a handler. The handler performs the actual I/O and *resumes* the
computation with the result. The key insight: the function does not know or care
*how* the effect is handled. It just declares what effects it needs.

This is similar to dependency injection, but with a crucial difference: algebraic
effects support *delimited continuations*. When a function performs an effect, the
handler receives not just the effect request but the *rest of the computation* as a
resumable continuation. This enables effects like exceptions (do not resume),
nondeterminism (resume multiple times), and async (resume later).

Rust's `async`/`.await` is actually an effect system for one specific effect:
suspension. An `async fn` yields control to the executor and is resumed when the
awaited future completes. The `?` operator is an effect for early return on error.
The Rust language team's Keyword Generics Initiative explicitly frames these as
effects and explores making functions generic over them.

Yoshua Wuyts's blog post ["Extending Rust's Effect System"](https://blog.yoshuawuyts.com/extending-rusts-effect-system/) identifies five effects in Rust: `async`, `unsafe`, `const`, `try` (?),
and generators. The Keyword Generics Initiative proposes making functions generic
over these effects, so `fn read<effect async>()` could be called from both sync
and async contexts.

The `effing-mad` crate demonstrates a working (nightly-only) implementation using
Rust coroutines. Effectful functions are coroutines that yield effect requests.
Handlers drive the coroutine, matching on yielded effects and resuming with
responses. The implementation is in
[github.com/rosefromthedead/effing-mad](https://github.com/rosefromthedead/effing-mad).

## Architecture Hints

1. **Trait-based approach (stable Rust)**: Define each effect as a trait. Effectful
   functions are generic over a type parameter bounded by the required effect traits.
   Handlers are concrete types implementing the traits. This gives you compile-time
   dispatch and zero overhead, but you lose the ability to resume continuations —
   effects are just dependency injection. This is the practical, production-ready
   approach.

2. **Coroutine-based approach (nightly Rust)**: Effectful functions are coroutines
   that yield `enum EffectRequest { ReadFile(String), Log(String), ... }` values.
   Handlers drive the coroutine in a loop, matching on the yielded request and
   providing the response via `coroutine.resume(response)`. This preserves delimited
   continuation semantics but requires nightly and loses some type safety (the
   handler must know which response type each request expects). Study `effing-mad`'s
   approach where each effect gets its own trait with an associated type for the
   response.

3. **HList-based composition**: When composing multiple effects, you need a way to
   say "this function requires effects A and B." With traits, this is just
   `A + B` bounds. With coroutines, you need an effect *set*. One approach is to use
   heterogeneous lists (HLists) from the `frunk` crate to represent the set of
   active effects, with type-level membership proofs. This is advanced type-level
   programming.

4. **The handler passing pattern**: Instead of global state, thread effectful
   functions by passing a `&mut Handler` argument (or using a type parameter). The
   handler struct holds the implementation of all effects. In tests, you construct
   a different handler. This pattern is simple, zero-cost, and works on stable Rust.

5. **Why true algebraic effects are impossible in stable Rust**: First-class effects
   require higher-kinded types (to abstract over `Result<T, E>` vs `Future<Output=T>`
   vs `Option<T>` — all of which are effect carriers). Rust has GATs but not full
   HKTs. Second, delimited continuations require capturing the call stack at a yield
   point, which Rust can only do with coroutines (nightly). Third, effect polymorphism
   ("this function is generic over whether it is async or sync") requires the Keyword
   Generics Initiative, which is still in the design phase.

## Starting Points

- **effing-mad crate**: [github.com/rosefromthedead/effing-mad](https://github.com/rosefromthedead/effing-mad) — working algebraic
  effects on nightly. Study `examples/basic.rs` for the simplest usage. The
  `#[effectful]` proc macro transforms functions into coroutines. Handlers use
  `effing_mad::handle` to drive effectful computations.
- **Blog: "Faking Algebraic Effects and Handlers With Traits"**: [blog.shtsoft.eu/2022/12/22/effect-trait-dp.html](https://blog.shtsoft.eu/2022/12/22/effect-trait-dp.html) — the trait-based
  design pattern approach on stable Rust.
- **Blog: "Extending Rust's Effect System"**: [blog.yoshuawuyts.com/extending-rusts-effect-system/](https://blog.yoshuawuyts.com/extending-rusts-effect-system/) — Yoshua Wuyts's analysis
  of Rust's five effects and the vision for keyword generics.
- **Blog: "Coroutines and Effects"**: [without.boats/blog/coroutines-and-effects/](https://without.boats/blog/coroutines-and-effects/) — withoutboats on the
  relationship between Rust's coroutine implementation and algebraic effects.
- **Blog: "A Universal Lowering Strategy for Control Effects in Rust"**: [abubalay.com/blog/2024/01/14/rust-effect-lowering](https://www.abubalay.com/blog/2024/01/14/rust-effect-lowering) — how effectful
  computations are lowered to Rust's coroutine frames.
- **Blog: "Effects in Rust (and Koka)"**: [aloso.foo/blog/2025-10-10-effects/](https://aloso.foo/blog/2025-10-10-effects/) — comparison of
  Rust's implicit effect system with Koka's explicit one.
- **Keyword Generics Initiative**: [rust-lang.github.io/keyword-generics-initiative/](https://rust-lang.github.io/keyword-generics-initiative/) — the
  official Rust initiative for effect-generic programming. Study
  `explainer/effect-generic-bounds-and-functions.html`.
- **Koka language**: [koka-lang.github.io/koka/doc/book.html](https://koka-lang.github.io/koka/doc/book.html) — the reference
  implementation of algebraic effects. Study how effects are declared, handled, and
  composed. The design directly inspires what Rust is trying to approximate.

## Going Further

- Implement a nondeterminism effect: `choose(options: &[T]) -> T` that explores all
  branches. The handler resumes the computation once per option and collects all
  possible outcomes. This requires true delimited continuations (coroutine approach
  with cloning).
- Implement an async effect: your `FileSystem` handler performs actual async I/O
  using `tokio`, and the effectful computation is transparently async without the
  effectful function needing to be `async fn`. Compare the ergonomics with
  `#[async_trait]`.
- Build a monad transformer stack equivalent: implement `StateT<ReaderT<IO>>` using
  your effect system and compare the boilerplate against the traditional monadic
  approach. Demonstrate that effects compose without transformers.
- Benchmark the trait-based approach against the coroutine-based approach. The trait
  approach should be zero-cost (monomorphized). The coroutine approach has overhead
  from yield/resume. Quantify the difference on a tight loop performing 1M effects.
- Prototype effect row polymorphism: encode effect sets as type-level lists using
  `frunk::HList` and implement membership proofs so that a function requiring
  `FileSystem + Console` can be called from a context that provides
  `FileSystem + Console + Random`.

## Resources

**Blog Posts**
- [Algebraic Effects for the Rest of Us](https://overreacted.io/algebraic-effects-for-the-rest-of-us/) — Dan Abramov's accessible introduction to algebraic effects
- [Extending Rust's Effect System](https://blog.yoshuawuyts.com/extending-rusts-effect-system/) — Yoshua Wuyts on Rust's five effects
- [Coroutines and Effects](https://without.boats/blog/coroutines-and-effects/) — withoutboats on coroutines as effect carriers
- [A Universal Lowering Strategy for Control Effects in Rust](https://www.abubalay.com/blog/2024/01/14/rust-effect-lowering) — compiler-level perspective
- [Faking Algebraic Effects and Handlers With Traits](https://blog.shtsoft.eu/2022/12/22/effect-trait-dp.html) — stable Rust design pattern
- [Effects in Rust (and Koka)](https://aloso.foo/blog/2025-10-10-effects/) — cross-language comparison

**Papers**
- Daan Leijen — "Algebraic Effects for Functional Programming" (MSR-TR-2016-29, Microsoft Research, 2016) — [PDF](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/08/algeff-tr-2016-v2.pdf)
- Daan Leijen — "Type Directed Compilation of Row-Typed Algebraic Effects" (POPL 2017) — [PDF](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/12/algeff.pdf)
- Daan Leijen — "Koka: Programming with Row Polymorphic Effect Types" (MSFP 2014) — [arXiv:1406.2061](https://arxiv.org/pdf/1406.2061)
- Andrej Bauer, Matija Pretnar — "Programming with Algebraic Effects and Handlers" (Journal of Logical and Algebraic Methods, 2015) — foundational theory of the Eff language
- Gordon Plotkin, Matija Pretnar — "Handlers of Algebraic Effects" (ESOP 2009) — the original handlers paper

**Source Code**
- [rosefromthedead/effing-mad](https://github.com/rosefromthedead/effing-mad) — algebraic effects for Rust via coroutines (nightly)
- [rust-lang/keyword-generics-initiative](https://github.com/rust-lang/keyword-generics-initiative) — Rust's official effect generics exploration
- [koka-lang/koka](https://github.com/koka-lang/koka) — reference implementation of algebraic effects in a production language
- [frunk](https://github.com/lloydmeta/frunk) — HList and coproduct types for type-level programming in Rust

**Documentation**
- [effing-mad docs](https://docs.rs/effing-mad/latest/effing_mad/) — API reference for the `#[effectful]` macro, `handle`, and `transform`
- [Keyword Generics Initiative Charter](https://rust-lang.github.io/keyword-generics-initiative/CHARTER.html) — design goals for effect generics in Rust
- [Effect-Generic Bounds and Functions](https://rust-lang.github.io/keyword-generics-initiative/explainer/effect-generic-bounds-and-functions.html) — proposed syntax and semantics

**Talks**
- Daan Leijen — "Algebraic Effect Handlers with Resources and Deep Finalization" (ICFP 2018) — advanced handler patterns
- Yoshua Wuyts — "Keyword Generics Progress Report" (RustConf 2023) — state of effect generics in Rust
