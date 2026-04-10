# Type Theory and Functional Programming — Reference Overview

## Why This Section Matters

Rust's ownership model is not a memory management trick. It is a linear type system — a formal concept from type theory that guarantees every value is used exactly once. When you understand that, the borrow checker stops feeling like a hostile compiler and starts feeling like a proof assistant that catches entire classes of bugs before they ship.

The same insight applies across the board. The `?` operator in Rust is monadic bind for `Result<T, E>`. Go's `if err != nil { return err }` is a manual, verbose version of the same pattern. `Option<T>` and `Result<T, E>` are algebraic data types — not just enums with data, but the specific mathematical structure that makes exhaustiveness checking possible. Rust's `async fn` desugars to a state machine that is, formally, a cooperative effect system. Serde's derive macros work because Rust's type inference is powerful enough to resolve implementations at compile time.

This section does not aim to make you a type theorist. It aims to give you the mental models that explain why languages are designed the way they are, why certain patterns are idiomatic in one language but not another, and how to use advanced type system features with intent rather than cargo-culting them from Stack Overflow.

The organizing question throughout is: **what is the compiler actually proving when your code compiles?**

## Subtopics

| # | Topic | Core Insight | Estimated Reading |
|---|-------|--------------|-------------------|
| 01 | [Type Inference and Hindley-Milner](./01-type-inference-and-hindley-milner/01-type-inference-and-hindley-milner.md) | Type inference is constraint solving — the compiler assigns type variables and unifies them | 90 min |
| 02 | [Algebraic Data Types](./02-algebraic-data-types/02-algebraic-data-types.md) | Types are sets; product and sum types compose those sets; exhaustiveness checking falls out for free | 75 min |
| 03 | [Category Theory for Programmers](./03-category-theory-for-programmers/03-category-theory-for-programmers.md) | Functor = mappable structure; Monad = sequenceable computation; you already use both every day | 100 min |
| 04 | [Higher-Kinded Types](./04-higher-kinded-types/04-higher-kinded-types.md) | Type constructors are functions at the type level; HKT lets you abstract over them | 80 min |
| 05 | [Dependent Types and Refinements](./05-dependent-types-and-refinements/05-dependent-types-and-refinements.md) | Types that carry value-level information catch invariant violations at compile time | 85 min |
| 06 | [Effect Systems](./06-effect-systems/06-effect-systems.md) | async/await, exceptions, and state are all effects; algebraic effects unify them | 90 min |
| 07 | [Linear Types and Ownership](./07-linear-types-and-ownership/07-linear-types-and-ownership.md) | Rust ownership IS linear types — the borrow checker is a theorem prover for resource safety | 80 min |

## The Abstraction Ladder

The hardest thing about this material is that the concepts are mutually referential — monads require functors, functors require category theory, category theory requires understanding what a type even is. This ladder provides a ground-up path from code you already write to the formal ideas behind it.

```
Level 0 — Code you write today
  Go:   if err != nil { return err }
  Rust: file.read_to_string(&mut buf)?

Level 1 — Recognizing the pattern
  This is sequencing computations that can fail.
  Every step passes its result to the next, or short-circuits.

Level 2 — Naming the pattern
  This is the Monad pattern: bind (>>=) chains computations.
  result.and_then(|v| next_computation(v))

Level 3 — Understanding the structure
  A Monad is a Functor with two operations: return and bind.
  The Monad laws ensure sequential composition is predictable.

Level 4 — The category theory picture
  Monads are monoids in the category of endofunctors.
  (Yes, this is that meme. It is also true and eventually useful.)

Level 5 — The type theory picture
  Monadic bind corresponds to substitution in a type theory.
  The do-notation / for-await is syntactic sugar over this.
```

The goal of this section is to take you from Level 0 to Level 4 with working code at every step. Level 5 is provided for completeness but is not required for production usefulness.

```
Algebraic Data Types
        │
        │  gives you the raw material for
        ▼
  Functors and Monads ◄──────────────────────────────────┐
        │                                                  │
        │  generalizing over type constructors requires    │ effect systems
        ▼                                                  │ are monadic
  Higher-Kinded Types                                     │
        │                                                  │
        │  HKT + value-level information gives             │
        ▼                                                  │
  Dependent Types ──── type-safe at boundaries ──────────┘
        │
        │  all of this rests on
        ▼
  Type Inference (Hindley-Milner)
        │
        │  the fundamental resource model comes from
        ▼
  Linear Types and Ownership
```

## Connecting to Languages You Know

**Go**: Go's type system is deliberately simple. No HKT, no typeclasses, limited type inference. The design decision is that Go code should be easy to read and understand without IDE assistance. The tradeoff is that FP patterns require more boilerplate in Go — you simulate typeclasses with interfaces, simulate HKT with indirection, and write error-handling patterns manually that Rust encodes in the type system. This is not a failure of Go; it is a documented design choice. Understanding the theory tells you when to reach for these patterns and when Go's simpler approach is genuinely better.

**Rust**: Rust's type system is far richer. Traits are Haskell typeclasses. GATs (Generic Associated Types) give limited HKT. Const generics give limited dependent types. The ownership system is a linear type system with relaxations for shared references. Rust's type system is powerful enough that `serde`, `tokio`, and `rayon` are implemented as libraries — there is no language magic. Understanding the theory makes these crates understandable at their source level.

## Time Investment

| Level | Description | Estimated Hours |
|-------|-------------|-----------------|
| Comprehension | Read all documents, trace through examples | 10–12 hours |
| Hands-on | Complete the 30-min and 2–4h exercises per topic | 35–50 hours |
| Proficiency | Complete the 4–8h exercises; apply to a real project | 70–90 hours |
| Deep | Complete 8–15h exercises; contribute to type-heavy crates | 130–170 hours |

## Prerequisites

Before this section, you should be comfortable with:

- **Go**: generics (1.18+), interfaces, composition, error handling; understanding of goroutines and channels is helpful for the effect systems section
- **Rust**: ownership and borrowing, traits, `Option` and `Result`, basic generics, pattern matching with `match`; experience with lifetimes is required for the linear types section
- **Mathematics**: basic set theory notation (∈, ∀, ∃) — nothing beyond what a discrete math course covers
- **Programming concepts**: recursion, higher-order functions, closures, parametric polymorphism

If you are not yet comfortable with Rust's ownership model, read the [Linear Types and Ownership](./07-linear-types-and-ownership/07-linear-types-and-ownership.md) section first — it provides the concrete grounding. Then return to the section overview.

The [Algebraic Data Types](./02-algebraic-data-types/02-algebraic-data-types.md) section is the best entry point if you want to start with something immediately practical.
