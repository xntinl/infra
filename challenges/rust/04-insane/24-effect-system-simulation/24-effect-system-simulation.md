# 24. Effect System Simulation

**Difficulty**: Insane

## Problem Statement

Algebraic effects are one of the most powerful abstractions in programming language theory. Languages like Koka, Eff, and OCaml 5 implement them natively, enabling structured control flow for side effects -- IO, state, exceptions, nondeterminism, concurrency -- without monadic boilerplate and with free composition. Rust has none of this. But Rust has traits, generics, associated types, and async/await (which is secretly a form of delimited continuations). That is enough.

Your mission is to build an **effect system simulation** in Rust that allows computations to declare, perform, and handle algebraic effects at the type level. A computation that performs `IO + State<i32> + Error<String>` effects should be a different type from one that performs only `IO + Error<String>`. Effect handlers should be composable: handle the `State` effect to get a pure `IO + Error<String>` computation, then handle `Error` to get an `IO` computation, then handle `IO` to get a pure value.

This is not a simple exercise in trait design. You will confront fundamental tensions in Rust's type system: the lack of higher-kinded types, the absence of row polymorphism, the interaction between effects and lifetimes, and the question of how to represent continuations in a language without GC. The result should be a usable (if verbose) library that demonstrates the core ideas of algebraic effects.

### What Are Algebraic Effects?

In a language with algebraic effects, a function can "perform" an effect operation, which suspends the function and passes control to the nearest enclosing "handler". The handler can inspect the operation, decide what to do, and optionally resume the suspended function with a value. This is strictly more powerful than exceptions (which don't resume) and monads (which don't compose freely).

```
// Pseudocode (Koka-style)
effect State<S> {
    get() -> S
    set(s: S) -> ()
}

effect Error<E> {
    raise(e: E) -> Never
}

fn example(): State<i32>, Error<String> -> String {
    let x = perform get();        // suspends, handler provides value
    if x < 0 {
        perform raise("negative") // suspends, handler decides fate
    }
    perform set(x + 1);
    format!("result: {}", x + 1)
}

// Handler eliminates one effect at a time
let result = handle[State<i32>](example, initial_state=0) {
    get(resume)    => resume(current_state)
    set(s, resume) => { current_state = s; resume(()) }
};
// result still has Error<String> effect

let final_val = handle[Error<String>](result) {
    raise(e) => format!("error: {}", e)  // no resume = exception-like
};
// final_val is pure: String
```

### Your Rust Encoding

You must design a Rust encoding of this system. There are multiple valid approaches; here is one possible skeleton:

#### Effect Traits

```rust
/// Marker trait for effect sets. Effects compose via nested types.
trait EffectSet {}

/// The empty effect set -- a pure computation.
struct Pure;
impl EffectSet for Pure {}

/// Cons-cell style effect composition.
struct With<E: Effect, Rest: EffectSet> {
    _phantom: PhantomData<(E, Rest)>,
}
impl<E: Effect, Rest: EffectSet> EffectSet for With<E, Rest> {}

/// An individual effect with its operations.
trait Effect {
    /// The type that describes operations this effect can perform.
    type Operation;
}
```

#### Effectful Computations

```rust
/// A computation that produces T with effects E.
/// This is the core type -- think of it as a "free monad" over E.
enum Eff<E: EffectSet, T> {
    /// Computation completed with a pure value.
    Done(T),
    /// Computation suspended, performing an effect operation.
    /// Contains the operation and a continuation (boxed closure).
    Perform {
        operation: ???,  // How to type this generically?
        continuation: ???, // fn(response) -> Eff<E, T>
    },
}
```

The central design challenge is the `Perform` variant. The operation type depends on which effect is being performed, and the continuation's input type depends on the operation's return type. You must find a way to encode this that Rust's type system accepts.

#### Effect Handlers

```rust
/// A handler for effect E within effect set Effects.
/// Transforms Eff<With<E, Rest>, T> into Eff<Rest, U>.
trait Handler<E: Effect, Rest: EffectSet, T> {
    type Output;

    /// Handle a completed computation.
    fn on_return(&self, value: T) -> Eff<Rest, Self::Output>;

    /// Handle a performed operation, with access to the continuation.
    fn on_perform(
        &self,
        operation: E::Operation,
        continuation: Box<dyn FnOnce(/* response */) -> Eff<With<E, Rest>, T>>,
    ) -> Eff<Rest, Self::Output>;
}
```

### Required Effects

Implement at least these five effects:

1. **State\<S\>**: Operations `Get -> S` and `Set(S) -> ()`. Handler maintains mutable state threaded through the continuation.

2. **Error\<E\>**: Operations `Raise(E) -> Never`. Handler can either propagate or recover. Does NOT resume the continuation (or resumes with a default).

3. **IO**: Operations `Print(String) -> ()` and `ReadLine -> String`. Handler provides the actual IO implementation or a mock.

4. **Reader\<R\>**: Operations `Ask -> R`. Handler injects an environment value. Simpler than State (no mutation).

5. **Nondeterminism**: Operations `Choose(Vec<T>) -> T`. Handler explores all choices, collecting results. This is the hardest one because it requires resuming the continuation multiple times (or simulating that).

### Composition Requirements

The system must demonstrate:

- **Effect polymorphism**: Write a function generic over its effect set. For example, `fn increment<E: HasState<i32>>() -> Eff<E, ()>` works with any effect set that includes `State<i32>`, regardless of what other effects are present.

- **Handler composition**: Handle effects one at a time, peeling them off the type. Start with `Eff<With<State<i32>, With<Error<String>, Pure>>, T>`, handle State to get `Eff<With<Error<String>, Pure>, (T, i32)>`, handle Error to get `Eff<Pure, Result<(T, i32), String>>`, then run the pure computation.

- **Effect independence**: The order in which effects are listed should not matter for the semantics (though it may matter for the type). `With<State<i32>, With<Error<String>, Pure>>` and `With<Error<String>, With<State<i32>, Pure>>` should both work, though handlers are applied in a specific order.

- **Effect reinterpretation**: Handle one effect by translating its operations into another effect. For example, handle `State<S>` by translating `Get` and `Set` into `Reader<Ref<S>>` operations plus mutation through an `UnsafeCell` or similar.

### The Continuation Problem

Algebraic effect handlers need access to the continuation -- the "rest of the computation" after the effect operation. In Rust, this is hard because:

- Closures that capture mutable state can't be cloned (needed for Nondeterminism, which resumes multiple times).
- Box<dyn FnOnce> can only be called once (fine for most effects, not for Nondeterminism).
- async/await provides a form of one-shot continuation via suspend/resume, but integrating it with the type-level effect tracking is non-trivial.

You must choose an approach and document its tradeoffs:

- **CPS (Continuation-Passing Style)**: Represent continuations as boxed closures. One-shot by default; clone via `Arc<Mutex<...>>` tricks for multi-shot.
- **Free monad / freer monad**: Represent the computation as a data structure (like `Eff` above) and interpret it. Continuations become `Box<dyn FnOnce(Response) -> Eff<E, T>>`.
- **Async-based**: Use async functions as the computation type. `perform` is an await point. Handlers are executors. Novel and tricky.
- **Coroutine/generator-based**: Use `std::ops::Coroutine` (nightly) or `genawaiter` to implement yield-based effects.

---

## Acceptance Criteria

### Effect Trait Hierarchy (AC-1)

- [ ] An `Effect` trait exists with an associated `Operation` type that enumerates the effect's operations
- [ ] An `EffectSet` trait (or equivalent) represents a collection of effects using type-level lists (e.g., `With<E, Rest>` and `Pure`)
- [ ] A `HasEffect<E>` constraint (or equivalent) allows functions to require a specific effect without naming the full set
- [ ] Effect sets with different orderings are both usable (even if they are distinct types, both can be handled)
- [ ] The `Pure` effect set represents a computation with no effects, equivalent to a plain value
- [ ] At least five distinct effect types are defined: `State<S>`, `Error<E>`, `IO`, `Reader<R>`, and `Nondeterminism`

### Effectful Computation Type (AC-2)

- [ ] An `Eff<Effects, T>` type (or equivalent) represents a computation producing `T` with effects `Effects`
- [ ] `Eff` has at least two variants: `Done(T)` for completed computations and `Perform { operation, continuation }` for suspended ones
- [ ] The continuation is represented as a callable that takes the operation's response type and returns a new `Eff`
- [ ] `Eff` supports monadic sequencing: `and_then` (flatmap) chains computations while preserving effect tracking
- [ ] `Eff::pure(value)` creates a `Done` computation
- [ ] Each effect provides a `perform_*` function that constructs the `Perform` variant with the right operation and a trivial continuation (e.g., `perform_get()` returns `Eff<With<State<S>, _>, S>`)

### Effect Handlers (AC-3)

- [ ] A `handle` function (or method) takes an `Eff<With<E, Rest>, T>` and a handler, producing `Eff<Rest, U>`
- [ ] The handler specifies behavior for `on_return` (what to do with the final value) and `on_perform` (what to do with each operation)
- [ ] `on_perform` receives the operation AND the continuation, allowing the handler to resume, abort, or transform the computation
- [ ] Handlers are composable: applying handler for `E1` then handler for `E2` reduces `Eff<With<E1, With<E2, Pure>>, T>` to `Eff<Pure, U>`
- [ ] A `run_pure` function extracts the value from `Eff<Pure, T>` (panicking or returning `Option` if effects remain unhandled)
- [ ] Handling is type-safe: attempting to handle an effect not in the set is a compile-time error

### State Effect (AC-4)

- [ ] `State<S>` defines operations `Get` (returns `S`) and `Set(S)` (returns `()`)
- [ ] `perform_get::<S>()` returns `Eff<With<State<S>, E>, S>` for any `E: EffectSet`
- [ ] `perform_set::<S>(value)` returns `Eff<With<State<S>, E>, ()>`
- [ ] The `StateHandler` takes an initial state and threads it through the computation via the continuation
- [ ] After handling, the final state is accessible (e.g., the output type becomes `(T, S)`)
- [ ] A test demonstrates a counter: get, increment, set, get, verify the value changed

### Error Effect (AC-5)

- [ ] `Error<E>` defines operation `Raise(E)` with return type `Never` (or `!` / `Infallible`)
- [ ] `perform_raise::<E>(error)` returns `Eff<With<Error<E>, Rest>, T>` for any `T` (since it never returns)
- [ ] The `ErrorHandler` does NOT resume the continuation on `Raise` -- it short-circuits
- [ ] After handling, the output type becomes `Result<T, E>` (or equivalent)
- [ ] Error and State compose: a computation with both effects can have State handled first (error preserves state changes) or Error handled first (error rolls back state), demonstrating that handler order affects semantics
- [ ] A test demonstrates try/catch behavior and the semantic difference of handler ordering

### IO Effect (AC-6)

- [ ] `IO` defines operations `Print(String)` (returns `()`) and `ReadLine` (returns `String`)
- [ ] A "real" IO handler performs actual stdout/stdin operations
- [ ] A "mock" IO handler records prints and provides scripted inputs, enabling deterministic testing
- [ ] An effectful computation using `IO` can be tested with the mock handler without any conditional compilation or runtime flags
- [ ] A test runs the same computation with both real and mock handlers, verifying identical logic

### Reader Effect (AC-7)

- [ ] `Reader<R>` defines operation `Ask` (returns `R`)
- [ ] The handler injects a fixed environment value, resuming the continuation with it every time `Ask` is performed
- [ ] `Reader` composes with `State` -- a computation can read config and maintain state simultaneously
- [ ] A test demonstrates dependency injection: a computation asks for a database URL and uses it, with the handler providing a test URL

### Nondeterminism Effect (AC-8)

- [ ] `Nondeterminism` defines operation `Choose(Vec<T>)` (returns `T`)
- [ ] The handler explores all choices, collecting all possible results into a `Vec<T>`
- [ ] This requires multi-shot continuations -- the handler resumes the continuation once per choice
- [ ] The chosen approach for multi-shot continuations is documented with its tradeoffs (clone, Arc, re-execution, etc.)
- [ ] A test demonstrates: `choose([1,2,3])` followed by `choose(["a","b"])` produces all 6 combinations
- [ ] If multi-shot continuations are infeasible in the chosen encoding, a clearly documented alternative (e.g., backtracking, iterative deepening) is provided with equivalent observable behavior

### Effect Polymorphism (AC-9)

- [ ] At least two functions are written that are generic over their effect set using a `HasEffect<E>` bound
- [ ] Example: `fn increment<E: HasEffect<State<i32>>>() -> Eff<E, ()>` works regardless of other effects in `E`
- [ ] Effect-polymorphic functions compose: `increment` can be called within a computation that also has `Error` and `IO` effects
- [ ] The `HasEffect` mechanism works through any nesting depth (not just the head of the effect list)
- [ ] A test demonstrates calling an effect-polymorphic function from two different effect contexts

### Effect Reinterpretation (AC-10)

- [ ] At least one example of effect reinterpretation: handling effect `E1` by translating its operations into effect `E2`
- [ ] Example: handle `State<S>` by translating to `Reader<Arc<Mutex<S>>>` operations
- [ ] The reinterpretation handler produces `Eff<With<E2, Rest>, T>` instead of `Eff<Rest, T>`
- [ ] A test verifies that the reinterpreted computation behaves identically to the direct implementation

### Composition and Ordering (AC-11)

- [ ] A computation with three or more effects is constructed and fully handled, one effect at a time
- [ ] The handling order is varied in tests to demonstrate that it affects the output type and semantics
- [ ] Specifically: `State + Error` handled as `State-then-Error` vs `Error-then-State` produces different result types and different behavior on error (state preserved vs state rolled back)
- [ ] A diagram or comment in the code explains the semantic difference
- [ ] All intermediate types are explicitly annotated (not inferred) to serve as documentation

### Performance and Practicality (AC-12)

- [ ] A simple benchmark compares the effect system against hand-written equivalent code (e.g., manual state threading vs `State` effect)
- [ ] The overhead is documented: the system should be usable for non-hot-path code, even if it is slower than manual implementations
- [ ] Stack depth is bounded: deeply nested `and_then` chains don't cause stack overflow (trampoline if needed)
- [ ] Compilation times are reasonable: the type-level machinery doesn't cause exponential compile time for moderate effect sets (3-5 effects)

---

## Starting Points

### The Freer Monad Approach

The "freer monad" (also called "operational monad") represents effectful computations as a data structure that can be interpreted by handlers. This is the most common approach in Haskell libraries like `freer-simple` and `polysemy`.

```rust
/// A computation producing T with effects from the set E.
enum Eff<E: EffectSet, T> {
    /// Pure value, computation complete.
    Pure(T),
    /// Perform an operation and continue with the result.
    /// The existential type of the operation's return value is
    /// hidden behind the continuation closure.
    Impure(Box<dyn EffOperation<E, T>>),
}

/// Type-erased effect operation with its continuation.
trait EffOperation<E: EffectSet, T> {
    /// Apply a handler, returning either a handled computation
    /// or an unhandled operation in the remaining effect set.
    fn handle<H, E1, Rest, U>(self: Box<Self>, handler: &H) -> Eff<Rest, U>
    where
        H: Handler<E1, Rest, T, Output = U>,
        E: Contains<E1, Remainder = Rest>;
}
```

The key insight: each `Impure` node stores an operation and a continuation `Box<dyn FnOnce(Response) -> Eff<E, T>>`, but the `Response` type varies per operation. You can hide this behind a trait object or an enum.

### The Async/Await Approach

Rust's async/await is syntactic sugar for state machines that suspend and resume -- which is exactly what effect operations do. The idea: effectful computations are async functions, `perform` is an `.await` on a special future, and handlers are custom executors.

```rust
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};

/// A "future" that represents performing an effect operation.
struct Perform<Op> {
    operation: Option<Op>,
    response: Option<Op::Response>,
}

impl<Op: Operation> Future for Perform<Op> {
    type Output = Op::Response;

    fn poll(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Self::Output> {
        if let Some(response) = self.response.take() {
            Poll::Ready(response)
        } else {
            // Store the operation somewhere the handler can see it
            // Wake will be called when the handler provides a response
            Poll::Pending
        }
    }
}
```

This approach has the advantage of using Rust's native suspend/resume mechanism, but the disadvantage of fighting the `Future` trait's API (which expects `Poll::Pending` to mean "try again later", not "handle this effect").

### Type-Level Effect Set Membership

To write effect-polymorphic functions, you need a way to assert "effect `E` is somewhere in effect set `S`":

```rust
/// Proof that effect E is in effect set S, with S minus E = Rest.
trait Contains<E: Effect>: EffectSet {
    type Remainder: EffectSet;

    /// Inject an E operation into this effect set.
    fn inject(op: E::Operation) -> Self::AnyOperation;

    /// Try to extract an E operation from this effect set.
    fn extract(op: Self::AnyOperation) -> Result<E::Operation, Self::Remainder::AnyOperation>;
}

// Base case: E is at the head
impl<E: Effect, Rest: EffectSet> Contains<E> for With<E, Rest> {
    type Remainder = Rest;
    // ...
}

// Recursive case: E is somewhere deeper
impl<E: Effect, Head: Effect, Rest: EffectSet> Contains<E> for With<Head, Rest>
where
    Rest: Contains<E>,
{
    type Remainder = With<Head, Rest::Remainder>;
    // ...
}
```

This requires careful handling of overlapping impls. You may need the `#[feature(specialization)]` or a workaround using auxiliary traits.

### Koka Language as Reference

Koka (https://koka-lang.github.io/) is the canonical reference for algebraic effects in practice. Its papers describe:

- Row-polymorphic effect types (the basis for effect polymorphism)
- Evidence-passing translation (how effects compile to efficient code)
- Handler semantics (especially for multi-shot continuations)

Read "Algebraic Effects for the Rest of Us" by Dan Abramov for an accessible introduction, then Daan Leijen's papers for the formal treatment.

### The frunk Crate for HList-Based Type Manipulation

The `frunk` crate provides heterogeneous lists (HLists) and type-level indexing that can serve as the foundation for effect sets:

```rust
use frunk::{HCons, HNil};

// Effect set as an HList
type MyEffects = HCons<State<i32>, HCons<Error<String>, HNil>>;

// frunk provides Index<N> for type-safe access at any position
```

This saves you from implementing the `Contains` trait from scratch, though you'll still need to build the effect machinery on top.

### Trampoline for Stack Safety

Deep `and_then` chains will overflow the stack without trampolining:

```rust
enum Trampoline<T> {
    Done(T),
    Bounce(Box<dyn FnOnce() -> Trampoline<T>>),
}

fn run<T>(mut t: Trampoline<T>) -> T {
    loop {
        match t {
            Trampoline::Done(v) => return v,
            Trampoline::Bounce(f) => t = f(),
        }
    }
}
```

Integrate this into `Eff` to ensure that handling a computation with 100,000 chained `and_then` calls doesn't blow the stack.

---

## Hints

1. **Start with State alone.** Get `State<i32>` working end-to-end: define the effect, construct a computation, handle it, extract the result. Only then add a second effect and tackle composition.

2. **The hardest design decision is how to type the continuation.** If `Get` returns `S` and `Set(S)` returns `()`, the continuation after `Get` has type `FnOnce(S) -> Eff<E, T>` while the continuation after `Set` has type `FnOnce(()) -> Eff<E, T>`. You need a way to store both in the same `Perform` variant. An enum per effect that includes the continuation is one approach.

3. **Don't fight Rust's type system -- encode around it.** If you find yourself wanting higher-kinded types, use the "defunctionalization" trick: represent type-level functions as marker structs with associated types. If you need GATs, use them (they're stable).

4. **The `Contains` / `HasEffect` trait will likely require a nightly feature** or a creative workaround. The problem is overlapping impls: "E is at the head" vs "E is deeper". Solutions include: (a) auxiliary index types, (b) the `frunk` approach with `Index<Here>` vs `Index<There<N>>`, (c) proc macros that generate non-overlapping impls.

5. **For Nondeterminism, accept the limitation.** True multi-shot continuations require cloning the entire computation state. In Rust, this means either: (a) requiring `Clone` on all captured state (very restrictive), (b) re-running the computation from scratch with different choices (backtracking), or (c) using `Arc` and structural sharing. Document whichever you choose.

6. **Handler ordering matters semantically, not just for types.** `handle_state(handle_error(comp))` means errors abort the whole computation (state changes are lost). `handle_error(handle_state(comp))` means the state handler wraps the error, so you get the state at the point of error. Draw the diagrams.

7. **Use `enum_dispatch` or manual vtable tricks** if trait object overhead in the `Perform` variant is a concern. The hot path of an effect system is the handler loop, and every `Perform` node involves dynamic dispatch.

8. **Consider a proc macro for ergonomics.** Defining effects by hand is verbose. A `#[effect]` attribute macro that generates the operation enum, perform functions, and HasEffect impls from a trait-like definition would make the library usable. But implement the manual version first.

9. **The "Evidence Passing" optimization from Koka** avoids the overhead of CPS by passing effect handler references directly through the call stack. In Rust, this could be modeled as passing `&dyn Handler` arguments, avoiding the allocation overhead of boxing continuations. This is an advanced optimization -- get the boxed version working first.

10. **Test handler composition with a "calculator" example:** `eval(expr)` uses `State<Vec<i32>>` for the stack, `Error<String>` for division by zero, `Reader<HashMap<String, i32>>` for variable bindings, and `IO` for logging intermediate results. Handle each effect in turn and verify the final result.

11. **If compilation times explode, reduce type-level recursion depth.** Support effect sets of up to 5-8 effects. Beyond that, the recursive trait resolution becomes a bottleneck. This is a known limitation of type-level programming in Rust.

12. **Study the `effing-mad` crate** (if it exists at time of reading) for prior art on algebraic effects in Rust. Also look at `eff` (Rust crate), though it may be unmaintained. Understanding their design decisions and limitations will save you from dead ends.

13. **The type signatures will be ugly.** Accept this. Algebraic effects in Rust require verbose types because Rust lacks row polymorphism. Type aliases are your friend: `type Stateful<T> = Eff<With<State<i32>, Pure>, T>`. Provide aliases for common effect combinations.

14. **For the async approach, look at how `embassy` or `cassette` implement custom executors.** The key insight is that you control the `Waker` and `Context`, so you can smuggle effect operation data through the waker mechanism. This is hacky but functional.

15. **Verify your understanding by implementing the same computation three ways:** (a) with your effect system, (b) with manual state-passing and Result, (c) with monad transformers (if you have a monad library). All three should produce identical results. The effect system version should be the most readable (despite the type noise).
