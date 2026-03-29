<!-- difficulty: intermediate-advanced -->
<!-- category: testing -->
<!-- languages: [rust] -->
<!-- concepts: [property-based-testing, random-generation, shrinking, traits, generics] -->
<!-- estimated_time: 6-8 hours -->
<!-- bloom_level: apply, analyze, create -->
<!-- prerequisites: [rust-traits, generics, closures, rng-basics, iterator-patterns] -->

# Challenge 57: Property-Based Test Framework

## Languages

Rust (stable, latest edition)

## Prerequisites

- Solid understanding of Rust traits and generics (defining and implementing trait bounds)
- Experience with closures and `Fn` trait family (`Fn`, `FnMut`)
- Familiarity with `rand` crate basics (Rng, distributions)
- Understanding of iterator patterns and combinators (`map`, `flat_map`, `filter`)

## Learning Objectives

- **Implement** a random value generation system using the Arbitrary pattern
- **Apply** trait-based abstraction to create composable generator combinators
- **Analyze** failing test cases to find minimal counterexamples through shrinking
- **Design** a seed-based reproducibility system for deterministic failure replay
- **Create** a property checking orchestrator with configurable test parameters

## The Challenge

Build a property-based testing framework inspired by QuickCheck and proptest. Instead of writing individual test cases by hand, property-based testing generates hundreds of random inputs and checks that a property (a boolean condition) holds for all of them. When a property fails, the framework *shrinks* the failing input to find the smallest counterexample that still triggers the failure.

Your framework must support: random value generators for primitive and compound types, an `Arbitrary` trait that users implement for custom types, property definition as closures, configurable test count and seed, and a basic shrinking engine that reduces failing inputs toward minimal counterexamples.

The key insight is that generators and shrinkers are closely related -- a good framework designs them together so that shrunk values maintain structural invariants from the generator.

## Requirements

1. Define a `Gen` struct wrapping an RNG with a configurable size parameter (controls complexity of generated values)
2. Implement an `Arbitrary` trait with `fn arbitrary(g: &mut Gen) -> Self` and `fn shrink(&self) -> Box<dyn Iterator<Item = Self>>`
3. Provide `Arbitrary` implementations for: `bool`, `u8`, `u16`, `u32`, `u64`, `i8`, `i16`, `i32`, `i64`, `f32`, `f64`, `char`, `String`, `Vec<T: Arbitrary>`, `Option<T: Arbitrary>`, `(A, B)` tuples
4. Implement a `forall` function: given a property `Fn(T) -> bool` and config, generate `n` random `T` values, check the property, and report pass/fail
5. On failure, invoke the shrink loop: repeatedly shrink the failing input, testing each candidate, keeping the smallest that still fails
6. Support seed-based reproducibility: given the same seed, the framework produces identical test runs
7. Provide a `Config` struct with: number of test cases (default 100), max shrink iterations (default 1000), seed (optional), size range
8. Return structured results: `TestResult` with pass count, fail info (original input, shrunk input, shrink steps), seed used
9. Implement at least two generator combinators: `one_of` (choose from a list of generators) and `such_that` (filter generated values by a predicate)
10. Write tests that verify: shrinking finds minimal counterexamples, seed reproducibility works, all Arbitrary implementations generate valid values

## Hints

<details>
<summary>Hint 1: The Gen struct</summary>

Wrap a seeded RNG and a size parameter. The size controls how "large" generated values are (longer strings, bigger numbers, deeper nesting):

```rust
use rand::rngs::StdRng;
use rand::SeedableRng;

pub struct Gen {
    rng: StdRng,
    size: usize,
}

impl Gen {
    pub fn new(seed: u64, size: usize) -> Self {
        Self {
            rng: StdRng::seed_from_u64(seed),
            size,
        }
    }
}
```

</details>

<details>
<summary>Hint 2: Shrinking strategy for integers</summary>

For integers, shrink toward zero by binary search. Yield candidates that are progressively closer to zero:

```rust
impl Arbitrary for i32 {
    fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
        let val = *self;
        if val == 0 {
            return Box::new(std::iter::empty());
        }
        let mut candidates = vec![0];
        let mut d = val;
        while d != 0 {
            d /= 2;
            if val - d != val && val - d != 0 {
                candidates.push(val - d);
            }
        }
        Box::new(candidates.into_iter())
    }
}
```

</details>

<details>
<summary>Hint 3: Shrinking collections</summary>

For `Vec<T>`, combine two shrinking strategies: (1) remove elements to produce shorter vectors, and (2) shrink individual elements in place. Try removals first (they reduce size faster), then element-wise shrinking:

```rust
// Removal: try removing each element
let removals = (0..self.len()).map(move |i| {
    let mut v = original.clone();
    v.remove(i);
    v
});
```

</details>

<details>
<summary>Hint 4: The shrink loop</summary>

The shrink loop is greedy: always take the first shrunk candidate that still fails the property. Repeat until no candidate fails or you hit the iteration limit:

```rust
fn shrink_loop<T: Arbitrary + Clone>(
    failing: T,
    property: &dyn Fn(&T) -> bool,
    max_iterations: usize,
) -> (T, usize) {
    let mut current = failing;
    let mut steps = 0;
    while steps < max_iterations {
        let mut found_smaller = false;
        for candidate in current.shrink() {
            steps += 1;
            if !property(&candidate) {
                current = candidate;
                found_smaller = true;
                break;
            }
        }
        if !found_smaller { break; }
    }
    (current, steps)
}
```

</details>

## Acceptance Criteria

- [ ] `Arbitrary` trait is implemented for all listed primitive and compound types
- [ ] `Gen` produces deterministic sequences given the same seed
- [ ] `forall` checks a property over `n` generated inputs and reports pass/fail
- [ ] Failing properties trigger the shrink loop, which finds a smaller counterexample
- [ ] Shrinking integers converges to values near zero
- [ ] Shrinking vectors produces shorter vectors and simpler elements
- [ ] `one_of` randomly selects from a list of values/generators
- [ ] `such_that` filters generated values by a predicate (with a max-attempts guard)
- [ ] `TestResult` includes the seed, original failing input, shrunk input, and shrink step count
- [ ] Re-running with the same seed reproduces the exact same test sequence and failure
- [ ] All tests pass with `cargo test`

## Research Resources

- [QuickCheck: A Lightweight Tool for Random Testing (Claessen & Hughes)](https://www.cs.tufts.edu/~nr/cs257/archive/john-hughes/quick.pdf) -- the original paper that started property-based testing
- [Hypothesis: A new approach to property-based testing](https://hypothesis.works/) -- modern PBT with integrated shrinking concepts
- [proptest book](https://proptest-rs.github.io/proptest/intro.html) -- Rust's proptest crate documentation, excellent design reference
- [Rust `rand` crate docs](https://docs.rs/rand/latest/rand/) -- random number generation in Rust
- [How to Specify It! (Lamport & Hughes)](https://www.youtube.com/watch?v=G0NUOst-53U) -- talk on writing good properties
- [Shrinking: The challenge](https://hypothesis.works/articles/integrated-shrinking/) -- why shrinking is hard and how Hypothesis solves it
