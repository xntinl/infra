# 14. Formal Verification with Kani, Creusot, and Prusti

**Difficulty**: Insane

## The Challenge

Implement a generic `RingBuffer<T, const N: usize>` — a fixed-capacity, stack-allocated circular buffer using `MaybeUninit<T>` for storage — and then formally verify its correctness using three complementary verification tools: Kani (bounded model checking), Creusot (deductive verification), and Prusti (Viper-based verification). Each tool attacks the problem from a different angle, and you will learn what each can and cannot prove.

The `RingBuffer` is the perfect verification target because it combines several hazards: `MaybeUninit` (uninitialized memory), index arithmetic that wraps (modular arithmetic bugs), `Drop` that must drop exactly the initialized elements (not more, not less), and `unsafe` for reading from `MaybeUninit`. A bug in any of these is immediate undefined behavior — and is extremely hard to catch with testing alone because the symptoms (reading garbage, double-free, memory leak) may only appear under specific sequences of push/pop operations.

Your verification goals are:
- **Kani**: Prove that for all sequences of up to 8 operations (push, pop, peek) on a `RingBuffer<u8, 4>`, no assertion fires, no out-of-bounds access occurs, and the buffer's length invariant holds. Kani uses bounded model checking via CBMC — it exhaustively explores all possible inputs within a bound.
- **Creusot**: Write Pearlite specifications (preconditions, postconditions, loop invariants) for `push`, `pop`, and `len`, and use the Why3 prover to verify them deductively. Creusot proves properties for *all* inputs, not just up to a bound.
- **Prusti**: Write `#[requires(...)]` and `#[ensures(...)]` annotations and verify with the Viper verification infrastructure. Prusti's auto-active approach lets you verify Rust code with pre/post conditions checked at the source level.

## Acceptance Criteria

- [ ] `RingBuffer<T, N>` is `#![no_std]` compatible — uses `[MaybeUninit<T>; N]` for storage, no heap
- [ ] `push(&mut self, value: T) -> Result<(), T>` returns `Err(value)` when full
- [ ] `pop(&mut self) -> Option<T>` returns `None` when empty
- [ ] `peek(&self) -> Option<&T>` returns reference to front element
- [ ] `len(&self)` always equals the number of successful pushes minus successful pops
- [ ] `is_empty()` and `is_full()` are consistent with `len()`
- [ ] `Drop` drops exactly `self.len()` elements — verified by Miri with a `DropCounter` type
- [ ] `Drop` is correct even when `T` is a ZST
- [ ] All `unsafe` blocks have `// SAFETY:` comments with invariant references
- [ ] **Kani**: At least 3 proof harnesses that verify bounded correctness
- [ ] **Kani**: One harness uses `kani::any()` to generate arbitrary `u8` values and operation sequences
- [ ] **Kani**: One harness verifies the `len` invariant: `len == pushes - pops` for all execution paths
- [ ] **Kani**: One harness verifies that `pop` after `push` returns the pushed value (FIFO property)
- [ ] **Creusot**: `push` and `pop` have Pearlite `#[requires]` and `#[ensures]` annotations
- [ ] **Creusot**: Loop invariant in `Drop` proves exactly `len` elements are dropped
- [ ] **Prusti**: At least 2 functions annotated with `#[requires]`/`#[ensures]` that verify successfully

## Background

### Kani — Bounded Model Checking

Kani translates Rust (via MIR) to a model that CBMC (C Bounded Model Checker) can verify. The key abstractions:

- `kani::any::<T>()` — generates a symbolic value of type `T`. CBMC explores all possible concrete values.
- `kani::assume(condition)` — constrains the symbolic search space. Use to filter invalid inputs.
- `kani::assert(condition, "message")` — asserts a property that must hold for all explored paths.
- `#[kani::proof]` — marks a function as a verification harness (analogous to `#[test]`).
- `#[kani::unwind(N)]` — sets the loop unwinding bound. Without this, CBMC may not terminate on loops.

Install: `cargo install --locked kani-verifier && cargo kani setup`
Run: `cargo kani --harness harness_name`

A typical Kani harness for a ring buffer:

```rust
#[kani::proof]
#[kani::unwind(10)]
fn verify_push_pop_roundtrip() {
    let mut rb: RingBuffer<u8, 4> = RingBuffer::new();
    let val: u8 = kani::any();
    assert!(rb.push(val).is_ok());
    assert_eq!(rb.pop(), Some(val));
}
```

Study the Kani documentation on nondeterministic variables and the `BoundedArbitrary` trait for generating bounded sequences. See also the "Lessons Learned So Far From Verifying the Rust Standard Library" paper (arXiv:2510.01072) for practical patterns.

### Creusot — Deductive Verification

Creusot translates Rust code into WhyML (the language of the Why3 verification platform) and uses SMT solvers (Z3, CVC5) to discharge proof obligations. Unlike Kani, Creusot proves properties for *all* inputs, not just bounded ones — but you must supply specifications.

Creusot's specification language is **Pearlite**, which extends Rust syntax with:

- `#[requires(precondition)]` — what must be true before calling the function
- `#[ensures(postcondition)]` — what is guaranteed after the function returns
- `result` — refers to the return value in postconditions
- `^self` (pronounced "final self") — the value of `self` at the end of the function (prophecy)
- `forall<x: T> condition` — universal quantification
- `==>` — logical implication

Creusot is a research tool from INRIA. The key paper is Denis et al. — "Creusot: A Foundry for the Deductive Verification of Rust Programs" (ICFEM 2022). Study the test suite at `creusot-rs/creusot/tests/should_succeed/` for working examples.

### Prusti — Auto-Active Verification

Prusti uses the Viper verification infrastructure to verify Rust programs. It supports `#[requires(...)]` and `#[ensures(...)]` annotations and `#[pure]` functions (used in specifications). Prusti's underlying logic is Implicit Dynamic Frames — it reasons about heap permissions and functional properties simultaneously.

Install: VS Code extension "Prusti Assistant" or build from source.

The paper "The Prusti Project: Formal Verification for Rust" (Astrauskas et al., CAV 2022) describes the architecture. Study `viperproject/prusti-dev/docs/user-guide/` for annotation syntax.

## Architecture Hints

```
src/
  lib.rs           // RingBuffer<T, N> implementation
  kani_proofs.rs   // #[kani::proof] harnesses (behind #[cfg(kani)])
  creusot_specs.rs // Pearlite specifications (behind #[cfg(creusot)])
  prusti_specs.rs  // Prusti annotations (behind #[cfg(prusti)])
tests/
  miri_tests.rs    // Drop correctness tests (run under Miri)
  property_tests.rs // proptest / quickcheck for cross-validation
```

The `RingBuffer` internal representation:

```
struct RingBuffer<T, const N: usize> {
    buf: [MaybeUninit<T>; N],
    head: usize,   // index of next element to read (pop)
    len: usize,    // number of initialized elements
}
```

The tail (write index) is computed as `(head + len) % N`. This avoids storing a separate tail pointer and makes the `len` invariant trivial: `len` is always the count of initialized elements, and `len <= N`.

## Going Further

- Verify a lock-free single-producer single-consumer ring buffer using Kani with `kani::spawn` for concurrent harnesses.
- Use Kani's contract features (`#[kani::requires]`, `#[kani::ensures]`) for modular verification — verify `push` and `pop` independently, then use contracts as assumptions when verifying callers.
- Extend the Creusot specification to prove that the ring buffer is a faithful model of a mathematical FIFO queue (using Pearlite sequences).
- Apply Kani to verify a real-world unsafe abstraction from your codebase — even a small one like a custom `NonNull` wrapper.
- Explore Kani's `#[kani::stub]` for replacing complex functions with verified abstractions during verification.
- Study the Rust Foundation's "Expanding the Rust Formal Verification Ecosystem" initiative and the ESBMC integration.

## Resources

- **Tool**: Kani Rust Verifier — [model-checking.github.io/kani](https://model-checking.github.io/kani/)
- **Repo**: Kani source — [github.com/model-checking/kani](https://github.com/model-checking/kani)
- **Docs**: Kani tutorial on nondeterministic variables — [model-checking.github.io/kani/tutorial-nondeterministic-variables](https://model-checking.github.io/kani/tutorial-nondeterministic-variables.html)
- **Docs**: Kani verification of Rust standard library — [model-checking.github.io/verify-rust-std/tools/kani](https://model-checking.github.io/verify-rust-std/tools/kani.html)
- **Paper**: arXiv — "Lessons Learned So Far From Verifying the Rust Standard Library" (2025) — [arxiv.org/html/2510.01072v2](https://arxiv.org/html/2510.01072v2)
- **Paper**: Denis et al. — "Creusot: A Foundry for the Deductive Verification of Rust Programs" (ICFEM 2022) — [inria.hal.science/hal-03737878](https://inria.hal.science/hal-03737878/document)
- **Repo**: Creusot — [github.com/creusot-rs/creusot](https://github.com/xldenis/creusot) — study `creusot/tests/should_succeed/`
- **Crate**: Pearlite specification library — [lib.rs/crates/pearlite](https://lib.rs/crates/pearlite)
- **Paper**: Astrauskas et al. — "The Prusti Project: Formal Verification for Rust" (CAV 2022) — [pm.inf.ethz.ch/publications](https://pm.inf.ethz.ch/publications/AstrauskasBilyFialaGrannanMathejaMuellerPoliSummers22.pdf)
- **Repo**: Prusti — [github.com/viperproject/prusti-dev](https://github.com/viperproject/prusti-dev)
- **Docs**: Prusti user guide — [viperproject.github.io/prusti-dev/user-guide](https://viperproject.github.io/prusti-dev/user-guide/basic.html)
- **Blog**: Colin Breck — "Making Even Safe Rust a Little Safer: Model Checking Safe and Unsafe Code" — [blog.colinbreck.com](https://blog.colinbreck.com/making-even-safe-rust-a-little-safer-model-checking-safe-and-unsafe-code/)
- **Foundation**: Rust Foundation — "Expanding the Rust Formal Verification Ecosystem: Welcoming ESBMC" — [rustfoundation.org/media](https://rustfoundation.org/media/expanding-the-rust-formal-verification-ecosystem-welcoming-esbmc/)
