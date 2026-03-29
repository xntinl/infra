<!-- difficulty: insane -->
<!-- category: testing -->
<!-- languages: [go, rust] -->
<!-- concepts: [integrated-shrinking, generator-combinators, coverage-guided-testing, stateful-testing, generics] -->
<!-- estimated_time: 30-45 hours -->
<!-- bloom_level: create, evaluate, synthesize -->
<!-- prerequisites: [property-based-testing-basics, generics-advanced, closures, rng, code-coverage-concepts] -->

# Challenge 145: Property Testing Shrinking Engine

## Languages

Go (1.22+) and Rust (stable, latest edition) -- both implementations required.

## Prerequisites

- Prior experience with property-based testing (QuickCheck, proptest, or rapid)
- Advanced generics: Go type parameters, Rust trait bounds with associated types
- Understanding of closures as first-class values and higher-order functions
- Familiarity with RNG seeding and deterministic generation
- Conceptual understanding of code coverage instrumentation

## Learning Objectives

- **Create** an integrated shrinking system where shrinking operates on the generator's random choices, not on generated values
- **Synthesize** composable generator combinators (map, flat_map, filter, one_of) that preserve shrinking invariants
- **Evaluate** coverage-guided property testing that steers generation toward unexplored code branches
- **Design** a stateful testing framework that generates operation sequences against a model and shrinks failing sequences

## The Challenge

Build an advanced property-based testing framework with integrated shrinking in both Go and Rust. Unlike value-based shrinking (QuickCheck-style, where each type defines how to shrink itself), integrated shrinking (Hedgehog-style) works by shrinking the *random choices* that the generator made. This means shrunk values automatically satisfy all generator invariants, because they are produced by replaying the generator with smaller random choices.

Implement generic generator combinators that compose cleanly: `map` (transform output), `flat_map` (dependent generation), `filter` (reject invalid values), `one_of` (weighted choice). Each combinator must preserve shrinking behavior through the choice sequence.

Add coverage-guided generation: instrument properties to track which code branches they exercise, then bias generation toward inputs that increase coverage. This catches bugs that pure random testing misses.

Implement stateful testing: generate sequences of operations (commands) against a system under test, compare behavior against a pure model, and shrink failing sequences to minimal reproductions.

## Requirements

1. Implement a `Rose` tree (lazy tree of shrink candidates) as the core shrinking data structure
2. Generators produce `Rose<T>` values: the root is the generated value, children are shrink candidates
3. Implement integrated shrinking: generators consume from a shared choice sequence (random bytes); shrinking replaces choices with smaller values and replays the generator
4. Implement combinators: `map`, `flat_map`, `filter` (with max-attempts), `one_of` (uniform and weighted), `frequency`
5. Generators for: integers (bounded), booleans, bytes, strings (unicode-aware), vectors (with size control), maps, optionals, recursive structures (with depth limit)
6. Implement a `forall` runner: generate N inputs, check property, on failure shrink the choice sequence to find the minimal failing input
7. Seed-based reproducibility with structured failure reporting (seed, original input, shrunk input, shrink steps)
8. Coverage-guided mode: track branch coverage during property execution, prefer inputs that increase total branch coverage
9. Stateful testing: define commands as `(precondition, apply_to_model, apply_to_system, postcondition)`, generate command sequences, check postconditions after each step, shrink failing sequences
10. Both Go and Rust implementations with equivalent functionality and idiomatic APIs

## Hints

The Rose tree is the key data structure. A `Rose<T>` is a value paired with a lazy iterator of subtrees, each representing a shrink candidate.

For coverage-guided mode, use compile-time instrumentation to track branch hits. In Rust, you can use `#[cfg(coverage)]` or runtime counters inserted into the property. In Go, use `testing.Coverage()` or manual counters.

## Acceptance Criteria

- [ ] Rose tree correctly represents a value with lazy shrink candidates
- [ ] Integrated shrinking produces valid values (all generator invariants hold in shrunk outputs)
- [ ] `map` preserves shrinking: shrunk inputs produce shrunk outputs
- [ ] `flat_map` composes generators with correct shrinking
- [ ] `filter` rejects invalid values while maintaining shrinking (filtered-out shrink candidates are skipped)
- [ ] `one_of` and `frequency` choose between generators with proper shrinking
- [ ] Integer generators shrink toward zero; string generators shrink toward empty
- [ ] Stateful testing generates command sequences and shrinks to minimal failing sequences
- [ ] Coverage-guided mode increases branch coverage over time compared to pure random
- [ ] Seed reproducibility works for both normal and stateful modes
- [ ] Go and Rust implementations pass equivalent test suites
- [ ] All tests pass with `cargo test` and `go test ./...`

## Research Resources

- [Hedgehog: A Modern Property-Based Testing System (Jacob Stanley)](https://hedgehog.qa/) -- the Haskell library that pioneered integrated shrinking
- [Hypothesis Internals: Integrated Shrinking](https://hypothesis.works/articles/integrated-shrinking/) -- David MacIver's explanation of why integrated shrinking matters
- [How to Specify It! (John Hughes)](https://www.youtube.com/watch?v=G0NUOst-53U) -- patterns for writing good stateful properties
- [American Fuzzy Lop: Technical Details](https://lcamtuf.coredump.cx/afl/technical_details.txt) -- coverage-guided fuzzing concepts applicable to property testing
- [proptest book: Shrinking](https://proptest-rs.github.io/proptest/proptest/tutorial/shrinking-basics.html) -- Rust proptest shrinking documentation
- [rapid (Go property testing)](https://pkg.go.dev/pgregory.net/rapid) -- Go library with integrated shrinking, good API reference
