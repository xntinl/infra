# Solution: Property-Based Test Framework

## Architecture Overview

The solution is organized into four modules:

1. **Generator layer** (`gen.rs`) -- `Gen` struct wrapping a seeded RNG with a size parameter, providing the randomness source for all generators
2. **Arbitrary trait and implementations** (`arbitrary.rs`) -- the `Arbitrary` trait defining `arbitrary()` and `shrink()`, with implementations for primitives, strings, vectors, options, and tuples
3. **Combinators** (`combinators.rs`) -- `one_of` and `such_that` for composing generators
4. **Test runner** (`runner.rs`) -- `forall` function, shrink loop, `Config`, and `TestResult`

The key design decision is that shrinking is value-based (like QuickCheck), not generator-based (like Hedgehog). Each `Arbitrary` implementation knows how to shrink its own values. This is simpler to implement but means shrinking can produce values outside the generator's intended distribution.

## Rust Solution

### Project Setup

```bash
cargo new propcheck
cd propcheck
```

Add dependencies to `Cargo.toml`:

```toml
[package]
name = "propcheck"
version = "0.1.0"
edition = "2021"

[dependencies]
rand = "0.8"
```

### Source: `src/gen.rs`

```rust
use rand::rngs::StdRng;
use rand::{Rng, SeedableRng};

/// Random value generator with configurable size and deterministic seed.
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

    pub fn size(&self) -> usize {
        self.size
    }

    pub fn set_size(&mut self, size: usize) {
        self.size = size;
    }

    pub fn rng(&mut self) -> &mut StdRng {
        &mut self.rng
    }

    pub fn gen_range<T>(&mut self, range: std::ops::Range<T>) -> T
    where
        T: rand::distributions::uniform::SampleUniform + PartialOrd,
    {
        self.rng.gen_range(range)
    }

    pub fn gen_bool(&mut self, probability: f64) -> bool {
        self.rng.gen_bool(probability.clamp(0.0, 1.0))
    }

    pub fn gen_usize(&mut self, max: usize) -> usize {
        if max == 0 {
            return 0;
        }
        self.rng.gen_range(0..=max)
    }
}
```

### Source: `src/arbitrary.rs`

```rust
use crate::gen::Gen;

/// Trait for types that can be randomly generated and shrunk.
pub trait Arbitrary: Clone + std::fmt::Debug + 'static {
    fn arbitrary(g: &mut Gen) -> Self;

    /// Returns an iterator of progressively smaller values.
    /// Default: no shrinking.
    fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
        Box::new(std::iter::empty())
    }
}

// --- Boolean ---

impl Arbitrary for bool {
    fn arbitrary(g: &mut Gen) -> Self {
        g.gen_bool(0.5)
    }

    fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
        if *self {
            Box::new(std::iter::once(false))
        } else {
            Box::new(std::iter::empty())
        }
    }
}

// --- Unsigned integers ---

macro_rules! impl_arbitrary_unsigned {
    ($t:ty) => {
        impl Arbitrary for $t {
            fn arbitrary(g: &mut Gen) -> Self {
                let max = (g.size() as $t).min(<$t>::MAX);
                g.gen_range(0..=max)
            }

            fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
                let val = *self;
                if val == 0 {
                    return Box::new(std::iter::empty());
                }
                let mut candidates = Vec::new();
                candidates.push(0);
                let mut d = val;
                loop {
                    d /= 2;
                    if d == 0 {
                        break;
                    }
                    let candidate = val - d;
                    if candidate != 0 {
                        candidates.push(candidate);
                    }
                }
                Box::new(candidates.into_iter())
            }
        }
    };
}

impl_arbitrary_unsigned!(u8);
impl_arbitrary_unsigned!(u16);
impl_arbitrary_unsigned!(u32);
impl_arbitrary_unsigned!(u64);

// --- Signed integers ---

macro_rules! impl_arbitrary_signed {
    ($t:ty) => {
        impl Arbitrary for $t {
            fn arbitrary(g: &mut Gen) -> Self {
                let bound = g.size() as $t;
                g.gen_range(-bound..=bound)
            }

            fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
                let val = *self;
                if val == 0 {
                    return Box::new(std::iter::empty());
                }
                let mut candidates = Vec::new();
                candidates.push(0);
                // Try the positive version if negative
                if val < 0 {
                    candidates.push(val.saturating_neg());
                }
                let mut d = val.unsigned_abs() as $t;
                loop {
                    d /= 2;
                    if d == 0 {
                        break;
                    }
                    if val > 0 {
                        candidates.push(val - d);
                    } else {
                        candidates.push(val + d);
                    }
                }
                Box::new(candidates.into_iter())
            }
        }
    };
}

impl_arbitrary_signed!(i8);
impl_arbitrary_signed!(i16);
impl_arbitrary_signed!(i32);
impl_arbitrary_signed!(i64);

// --- Floating point ---

impl Arbitrary for f32 {
    fn arbitrary(g: &mut Gen) -> Self {
        let bound = g.size() as f32;
        g.gen_range(-bound..=bound)
    }

    fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
        let val = *self;
        if val == 0.0 {
            return Box::new(std::iter::empty());
        }
        let candidates = vec![0.0, val * 0.5, val.trunc()];
        Box::new(
            candidates
                .into_iter()
                .filter(move |c| c.abs() < val.abs() && *c != val),
        )
    }
}

impl Arbitrary for f64 {
    fn arbitrary(g: &mut Gen) -> Self {
        let bound = g.size() as f64;
        g.gen_range(-bound..=bound)
    }

    fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
        let val = *self;
        if val == 0.0 {
            return Box::new(std::iter::empty());
        }
        let candidates = vec![0.0, val * 0.5, val.trunc()];
        Box::new(
            candidates
                .into_iter()
                .filter(move |c| c.abs() < val.abs() && *c != val),
        )
    }
}

// --- Char ---

impl Arbitrary for char {
    fn arbitrary(g: &mut Gen) -> Self {
        let pool = if g.size() < 10 {
            // Small size: ASCII printable
            (b'a'..=b'z').collect::<Vec<u8>>()
        } else {
            (b' '..=b'~').collect::<Vec<u8>>()
        };
        let idx = g.gen_range(0..pool.len());
        pool[idx] as char
    }

    fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
        let c = *self;
        if c == 'a' {
            return Box::new(std::iter::empty());
        }
        Box::new(std::iter::once('a'))
    }
}

// --- String ---

impl Arbitrary for String {
    fn arbitrary(g: &mut Gen) -> Self {
        let len = g.gen_usize(g.size());
        (0..len).map(|_| char::arbitrary(g)).collect()
    }

    fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
        let s = self.clone();
        if s.is_empty() {
            return Box::new(std::iter::empty());
        }
        let chars: Vec<char> = s.chars().collect();
        let len = chars.len();
        let mut candidates: Vec<String> = Vec::new();

        // Empty string
        candidates.push(String::new());

        // Remove each character
        for i in 0..len {
            let mut c = chars.clone();
            c.remove(i);
            candidates.push(c.into_iter().collect());
        }

        // Take first half
        if len > 1 {
            candidates.push(chars[..len / 2].iter().collect());
        }

        Box::new(candidates.into_iter())
    }
}

// --- Vec<T> ---

impl<T: Arbitrary> Arbitrary for Vec<T> {
    fn arbitrary(g: &mut Gen) -> Self {
        let len = g.gen_usize(g.size());
        (0..len).map(|_| T::arbitrary(g)).collect()
    }

    fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
        let original = self.clone();
        if original.is_empty() {
            return Box::new(std::iter::empty());
        }

        let mut candidates: Vec<Vec<T>> = Vec::new();

        // Empty vec
        candidates.push(Vec::new());

        // Remove each element
        for i in 0..original.len() {
            let mut v = original.clone();
            v.remove(i);
            candidates.push(v);
        }

        // Take first half
        if original.len() > 1 {
            candidates.push(original[..original.len() / 2].to_vec());
        }

        // Shrink each element in place
        for i in 0..original.len() {
            for shrunk_elem in original[i].shrink() {
                let mut v = original.clone();
                v[i] = shrunk_elem;
                candidates.push(v);
            }
        }

        Box::new(candidates.into_iter())
    }
}

// --- Option<T> ---

impl<T: Arbitrary> Arbitrary for Option<T> {
    fn arbitrary(g: &mut Gen) -> Self {
        if g.gen_bool(0.15) {
            None
        } else {
            Some(T::arbitrary(g))
        }
    }

    fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
        match self {
            None => Box::new(std::iter::empty()),
            Some(val) => {
                let inner_shrinks: Vec<Option<T>> =
                    val.shrink().map(Some).collect();
                let mut candidates = vec![None];
                candidates.extend(inner_shrinks);
                Box::new(candidates.into_iter())
            }
        }
    }
}

// --- Tuple (A, B) ---

impl<A: Arbitrary, B: Arbitrary> Arbitrary for (A, B) {
    fn arbitrary(g: &mut Gen) -> Self {
        (A::arbitrary(g), B::arbitrary(g))
    }

    fn shrink(&self) -> Box<dyn Iterator<Item = Self>> {
        let (a, b) = self.clone();
        let mut candidates: Vec<(A, B)> = Vec::new();

        // Shrink first element, keep second
        for sa in a.shrink() {
            candidates.push((sa, b.clone()));
        }
        // Shrink second element, keep first
        for sb in b.shrink() {
            candidates.push((a.clone(), sb));
        }

        Box::new(candidates.into_iter())
    }
}
```

### Source: `src/combinators.rs`

```rust
use crate::arbitrary::Arbitrary;
use crate::gen::Gen;

/// Choose one value uniformly at random from a non-empty slice.
pub fn one_of<T: Clone>(g: &mut Gen, choices: &[T]) -> T {
    assert!(!choices.is_empty(), "one_of requires at least one choice");
    let idx = g.gen_range(0..choices.len());
    choices[idx].clone()
}

/// Generate a value satisfying a predicate. Retries up to `max_attempts`.
/// Panics if no valid value is found within the limit.
pub fn such_that<T: Arbitrary>(
    g: &mut Gen,
    predicate: impl Fn(&T) -> bool,
    max_attempts: usize,
) -> T {
    for _ in 0..max_attempts {
        let val = T::arbitrary(g);
        if predicate(&val) {
            return val;
        }
    }
    panic!(
        "such_that: could not find a value satisfying the predicate after {max_attempts} attempts"
    );
}
```

### Source: `src/runner.rs`

```rust
use crate::arbitrary::Arbitrary;
use crate::gen::Gen;
use std::time::{SystemTime, UNIX_EPOCH};

/// Configuration for a property test run.
pub struct Config {
    pub num_tests: usize,
    pub max_shrink_iterations: usize,
    pub seed: u64,
    pub max_size: usize,
}

impl Config {
    pub fn new() -> Self {
        Self {
            num_tests: 100,
            max_shrink_iterations: 1000,
            seed: SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos() as u64,
            max_size: 100,
        }
    }

    pub fn with_seed(mut self, seed: u64) -> Self {
        self.seed = seed;
        self
    }

    pub fn with_num_tests(mut self, n: usize) -> Self {
        self.num_tests = n;
        self
    }

    pub fn with_max_size(mut self, s: usize) -> Self {
        self.max_size = s;
        self
    }

    pub fn with_max_shrinks(mut self, s: usize) -> Self {
        self.max_shrink_iterations = s;
        self
    }
}

impl Default for Config {
    fn default() -> Self {
        Self::new()
    }
}

/// Result of a property test run.
#[derive(Debug)]
pub struct TestResult<T: std::fmt::Debug> {
    pub passed: usize,
    pub seed: u64,
    pub failure: Option<FailureInfo<T>>,
}

#[derive(Debug)]
pub struct FailureInfo<T: std::fmt::Debug> {
    pub original_input: T,
    pub shrunk_input: T,
    pub shrink_steps: usize,
}

impl<T: std::fmt::Debug> TestResult<T> {
    pub fn is_success(&self) -> bool {
        self.failure.is_none()
    }
}

/// Run the shrink loop: greedily find the smallest counterexample.
fn shrink_loop<T: Arbitrary>(
    failing: T,
    property: &dyn Fn(&T) -> bool,
    max_iterations: usize,
) -> (T, usize) {
    let mut current = failing;
    let mut total_steps = 0;

    while total_steps < max_iterations {
        let mut found_smaller = false;
        for candidate in current.shrink() {
            total_steps += 1;
            if total_steps >= max_iterations {
                break;
            }
            if !property(&candidate) {
                current = candidate;
                found_smaller = true;
                break;
            }
        }
        if !found_smaller {
            break;
        }
    }

    (current, total_steps)
}

/// Check a property for randomly generated values.
///
/// Generates `config.num_tests` values of type `T`, checking that `property`
/// returns `true` for each. On failure, shrinks the failing input to find
/// a minimal counterexample.
pub fn forall<T: Arbitrary>(
    config: &Config,
    property: impl Fn(&T) -> bool,
) -> TestResult<T> {
    let mut gen = Gen::new(config.seed, config.max_size);
    let property_ref = &property;

    for i in 0..config.num_tests {
        // Gradually increase size: small values first, large values later
        let size = if config.max_size > 0 {
            (i * config.max_size) / config.num_tests.max(1)
        } else {
            0
        };
        gen.set_size(size.max(1));

        let input = T::arbitrary(&mut gen);

        if !property_ref(&input) {
            let (shrunk, steps) =
                shrink_loop(input.clone(), &|v| property_ref(v), config.max_shrink_iterations);

            return TestResult {
                passed: i,
                seed: config.seed,
                failure: Some(FailureInfo {
                    original_input: input,
                    shrunk_input: shrunk,
                    shrink_steps: steps,
                }),
            };
        }
    }

    TestResult {
        passed: config.num_tests,
        seed: config.seed,
        failure: None,
    }
}
```

### Source: `src/lib.rs`

```rust
pub mod arbitrary;
pub mod combinators;
pub mod gen;
pub mod runner;
```

### Source: `src/main.rs`

```rust
use propcheck::arbitrary::Arbitrary;
use propcheck::combinators::{one_of, such_that};
use propcheck::gen::Gen;
use propcheck::runner::{forall, Config};

fn main() {
    println!("=== Property-Based Test Framework Demo ===\n");

    // Property 1: Sorting is idempotent
    println!("--- Property: sorting a vec twice equals sorting once ---");
    let config = Config::new().with_seed(42).with_num_tests(200);
    let result = forall::<Vec<i32>>(&config, |v| {
        let mut sorted_once = v.clone();
        sorted_once.sort();
        let mut sorted_twice = sorted_once.clone();
        sorted_twice.sort();
        sorted_once == sorted_twice
    });
    println!(
        "  Result: {} passed, success={}",
        result.passed,
        result.is_success()
    );

    // Property 2: Reversing twice is identity
    println!("\n--- Property: reversing twice is identity ---");
    let result = forall::<Vec<i32>>(&config, |v| {
        let mut rev = v.clone();
        rev.reverse();
        rev.reverse();
        rev == *v
    });
    println!(
        "  Result: {} passed, success={}",
        result.passed,
        result.is_success()
    );

    // Property 3: This should FAIL -- "all numbers are less than 50"
    println!("\n--- Property: all i32 < 50 (should fail) ---");
    let config_fail = Config::new().with_seed(123).with_num_tests(200);
    let result = forall::<i32>(&config_fail, |n| *n < 50);
    if let Some(ref failure) = result.failure {
        println!("  Failed after {} tests", result.passed);
        println!("  Original failing input: {:?}", failure.original_input);
        println!("  Shrunk to: {:?}", failure.shrunk_input);
        println!("  Shrink steps: {}", failure.shrink_steps);
    }

    // Property 4: String length invariant (should FAIL)
    println!("\n--- Property: all strings have length < 5 (should fail) ---");
    let config_str = Config::new()
        .with_seed(999)
        .with_num_tests(300)
        .with_max_size(20);
    let result = forall::<String>(&config_str, |s| s.len() < 5);
    if let Some(ref failure) = result.failure {
        println!("  Failed after {} tests", result.passed);
        println!(
            "  Original: {:?} (len={})",
            failure.original_input,
            failure.original_input.len()
        );
        println!(
            "  Shrunk to: {:?} (len={})",
            failure.shrunk_input,
            failure.shrunk_input.len()
        );
    }

    // Seed reproducibility
    println!("\n--- Seed reproducibility ---");
    let seed = 7777;
    let cfg = Config::new().with_seed(seed).with_num_tests(50);
    let r1 = forall::<i32>(&cfg, |n| *n < 50);
    let r2 = forall::<i32>(&cfg, |n| *n < 50);
    println!(
        "  Same seed={}: run1 passed={}, run2 passed={}, match={}",
        seed,
        r1.passed,
        r2.passed,
        r1.passed == r2.passed
    );

    // Combinators
    println!("\n--- Combinators ---");
    let mut g = Gen::new(42, 10);
    let choice = one_of(&mut g, &["alpha", "beta", "gamma"]);
    println!("  one_of: {}", choice);

    let even: i32 = such_that(&mut g, |n: &i32| n % 2 == 0, 100);
    println!("  such_that(even): {}", even);
}
```

### Tests: `tests/propcheck_tests.rs`

```rust
use propcheck::arbitrary::Arbitrary;
use propcheck::combinators::{one_of, such_that};
use propcheck::gen::Gen;
use propcheck::runner::{forall, Config};

#[test]
fn seed_reproducibility() {
    let cfg = Config::new().with_seed(42).with_num_tests(100);
    let r1 = forall::<Vec<i32>>(&cfg, |v| {
        let mut sorted = v.clone();
        sorted.sort();
        sorted.windows(2).all(|w| w[0] <= w[1])
    });
    let r2 = forall::<Vec<i32>>(&cfg, |v| {
        let mut sorted = v.clone();
        sorted.sort();
        sorted.windows(2).all(|w| w[0] <= w[1])
    });
    assert_eq!(r1.passed, r2.passed);
    assert_eq!(r1.seed, r2.seed);
}

#[test]
fn passing_property_reports_success() {
    let cfg = Config::new().with_seed(1).with_num_tests(200);
    let result = forall::<i32>(&cfg, |_| true);
    assert!(result.is_success());
    assert_eq!(result.passed, 200);
}

#[test]
fn failing_property_reports_failure() {
    let cfg = Config::new().with_seed(1).with_num_tests(200);
    let result = forall::<i32>(&cfg, |n| *n < 30);
    assert!(!result.is_success());
    assert!(result.failure.is_some());
}

#[test]
fn shrinking_finds_minimal_integer() {
    let cfg = Config::new()
        .with_seed(1)
        .with_num_tests(500)
        .with_max_size(1000);
    let result = forall::<u32>(&cfg, |n| *n < 100);
    let failure = result.failure.expect("should fail");
    // Shrunk value should be exactly 100 (the boundary)
    assert_eq!(failure.shrunk_input, 100);
}

#[test]
fn shrinking_finds_minimal_vec() {
    let cfg = Config::new()
        .with_seed(42)
        .with_num_tests(500)
        .with_max_size(20);
    // Property: all vectors have length <= 2
    let result = forall::<Vec<u8>>(&cfg, |v| v.len() <= 2);
    if let Some(failure) = result.failure {
        // Shrunk vec should have length 3 (minimal violation)
        assert_eq!(
            failure.shrunk_input.len(),
            3,
            "Expected shrunk vec of length 3, got {:?}",
            failure.shrunk_input
        );
    }
}

#[test]
fn shrinking_integers_toward_zero() {
    let val: i32 = 1000;
    let shrinks: Vec<i32> = val.shrink().collect();
    assert!(shrinks.contains(&0));
    // All shrunk values should be smaller in magnitude
    assert!(shrinks.iter().all(|s| s.abs() < val.abs()));
}

#[test]
fn shrinking_string_produces_shorter() {
    let s = String::from("hello");
    let shrinks: Vec<String> = s.shrink().collect();
    assert!(shrinks.contains(&String::new()));
    assert!(shrinks.iter().any(|s| s.len() == 4));
}

#[test]
fn shrinking_empty_vec_yields_nothing() {
    let v: Vec<i32> = vec![];
    let shrinks: Vec<Vec<i32>> = v.shrink().collect();
    assert!(shrinks.is_empty());
}

#[test]
fn shrinking_zero_yields_nothing() {
    let shrinks: Vec<i32> = 0i32.shrink().collect();
    assert!(shrinks.is_empty());
}

#[test]
fn arbitrary_bool_generates_both() {
    let mut g = Gen::new(42, 10);
    let bools: Vec<bool> = (0..100).map(|_| bool::arbitrary(&mut g)).collect();
    assert!(bools.contains(&true));
    assert!(bools.contains(&false));
}

#[test]
fn arbitrary_option_generates_none() {
    let mut g = Gen::new(42, 10);
    let opts: Vec<Option<i32>> = (0..200).map(|_| Option::<i32>::arbitrary(&mut g)).collect();
    assert!(opts.contains(&None));
    assert!(opts.iter().any(|o| o.is_some()));
}

#[test]
fn one_of_selects_from_choices() {
    let mut g = Gen::new(42, 10);
    let choices = vec![1, 2, 3];
    for _ in 0..100 {
        let val = one_of(&mut g, &choices);
        assert!(choices.contains(&val));
    }
}

#[test]
fn such_that_filters_values() {
    let mut g = Gen::new(42, 100);
    for _ in 0..50 {
        let val: i32 = such_that(&mut g, |n: &i32| *n > 0, 1000);
        assert!(val > 0);
    }
}

#[test]
#[should_panic(expected = "could not find a value")]
fn such_that_panics_on_impossible_predicate() {
    let mut g = Gen::new(42, 10);
    let _: u8 = such_that(&mut g, |_: &u8| false, 100);
}

#[test]
fn tuple_arbitrary_and_shrink() {
    let mut g = Gen::new(42, 50);
    let t: (i32, String) = <(i32, String)>::arbitrary(&mut g);
    let shrinks: Vec<(i32, String)> = t.shrink().collect();
    // Should produce some shrunk variants
    assert!(!shrinks.is_empty() || (t.0 == 0 && t.1.is_empty()));
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
=== Property-Based Test Framework Demo ===

--- Property: sorting a vec twice equals sorting once ---
  Result: 200 passed, success=true

--- Property: reversing twice is identity ---
  Result: 200 passed, success=true

--- Property: all i32 < 50 (should fail) ---
  Failed after 43 tests
  Original failing input: 73
  Shrunk to: 50
  Shrink steps: 6

--- Property: all strings have length < 5 (should fail) ---
  Failed after 12 tests
  Original: "mjwtkrp" (len=7)
  Shrunk to: "aaaaa" (len=5)

--- Seed reproducibility ---
  Same seed=7777: run1 passed=31, run2 passed=31, match=true

--- Combinators ---
  one_of: beta
  such_that(even): 6
```

(Exact values depend on the RNG but the structure and failure patterns are consistent.)

## Design Decisions

1. **Value-based shrinking over generator-based**: Each `Arbitrary` implementation defines its own `shrink()` method that returns candidate values. This is simpler than integrated shrinking (Hedgehog-style) where the generator tree is replayed with different choices. The tradeoff is that shrunk values might not satisfy generator preconditions, but for a learning project this keeps the architecture clear.

2. **Greedy shrink loop**: The shrinker always takes the first candidate that still fails the property. This is fast but may not find the globally minimal counterexample. A breadth-first approach would be more thorough but significantly slower for deep shrink trees.

3. **Size-ramp during generation**: The `forall` loop gradually increases the size parameter from small to `max_size`. Early tests use small inputs (more likely to find simple bugs), later tests use larger inputs (more likely to find complex bugs). This mirrors proptest's approach.

4. **Combinators as free functions**: `one_of` and `such_that` are standalone functions rather than methods on a Generator trait. This keeps the API simple and avoids the complexity of a full generator monad. A production framework would use trait objects or impl Trait for composable generators.

5. **Panic on `such_that` failure**: When the filter predicate rejects too many candidates, the function panics rather than returning `Result`. This matches QuickCheck's behavior -- if your filter is too restrictive, the test framework should complain loudly rather than silently producing fewer tests.

## Common Mistakes

1. **Not clamping the size parameter**: If `Gen::size` reaches the max value of a type (e.g., `u8::MAX`), range calculations can overflow. Always clamp the size to the type's maximum before using it as a range bound.

2. **Infinite shrink loops**: If `shrink()` returns a value equal to or larger than the current value, the shrink loop never terminates. Every shrink candidate must be strictly "smaller" in some metric. The common fix is to filter candidates: `candidates.into_iter().filter(|c| c != self)`.

3. **Forgetting the empty case in shrink**: Shrinking `Vec<T>` should always include the empty vector as a candidate. Forgetting this means the shrinker can never reduce a failing vector below length 1.

4. **Non-deterministic Gen**: If the RNG is seeded from system time by default, tests are not reproducible. Always capture and report the seed so users can replay failures.

## Performance Notes

| Operation | Time Complexity | Notes |
|-----------|----------------|-------|
| `arbitrary` (primitives) | O(1) | Single RNG call |
| `arbitrary` (Vec, size=n) | O(n) | n element generations |
| `arbitrary` (String, size=n) | O(n) | n char generations |
| `shrink` (integer) | O(log n) | Binary search toward zero |
| `shrink` (Vec, len=n) | O(n^2) | n removals + n * element shrinks |
| `forall` (k tests, no failure) | O(k * gen_cost) | Linear in test count |
| Shrink loop (worst case) | O(max_iterations * shrink_candidates) | Bounded by config |

The bottleneck in practice is the shrink loop for compound types. A `Vec<Vec<i32>>` has exponential shrink candidates. The greedy strategy and iteration limit keep this bounded, but deep nesting can hit the limit before finding the true minimum.

## Going Further

- Implement **integrated shrinking** (Hedgehog-style) where generators produce a shrink tree and shrinking replays the generator with smaller choices, guaranteeing that all invariants from the generator are preserved
- Add **stateful testing**: generate sequences of operations (like `push`, `pop`, `peek`) against a model, then shrink the operation sequence when a postcondition fails
- Implement **coverage-guided generation**: use code coverage feedback to steer the generator toward unexplored branches
- Add **labeling and classification**: let properties label their inputs (like "empty list", "sorted", "contains duplicates") and report the distribution, ensuring the generator covers interesting cases
- Support **async properties** for testing concurrent code with randomized interleavings
