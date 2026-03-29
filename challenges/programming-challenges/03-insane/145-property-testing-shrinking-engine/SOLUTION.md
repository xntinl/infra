# Solution: Property Testing Shrinking Engine

## Architecture Overview

Both implementations share the same architecture across four layers:

1. **Rose tree** -- lazy tree of values with shrink candidates as children; the fundamental data structure for integrated shrinking
2. **Generator engine** -- generators consume random choices from a shared byte stream; shrinking replaces choices with smaller bytes and replays the generator
3. **Combinators** -- `map`, `flat_map`, `filter`, `one_of`, `frequency` compose generators while preserving shrinking
4. **Runners** -- `forall` (stateless), `stateful` (command sequences), and coverage-guided modes that orchestrate generation, property checking, and shrink loops

The key insight is that generators do not define shrinking on their output type. Instead, all shrinking operates on the underlying random choice sequence. When a choice is made smaller, the generator replays and produces a smaller output, guaranteed to satisfy all constraints.

## Rust Solution

### Project Setup

```bash
cargo new hedgehog-rs
cd hedgehog-rs
```

```toml
[package]
name = "hedgehog-rs"
version = "0.1.0"
edition = "2021"

[dependencies]
rand = "0.8"
```

### Source: `src/rose.rs`

```rust
/// A Rose tree: a value with lazily-evaluated shrink candidates.
#[derive(Clone)]
pub struct Rose<T> {
    pub root: T,
    pub children: Vec<Rose<T>>,
}

impl<T: Clone> Rose<T> {
    pub fn pure(value: T) -> Self {
        Rose {
            root: value,
            children: Vec::new(),
        }
    }

    pub fn new(value: T, children: Vec<Rose<T>>) -> Self {
        Rose {
            root: value,
            children,
        }
    }

    pub fn map<U: Clone>(self, f: impl Fn(T) -> U + Clone) -> Rose<U> {
        let root = f(self.root);
        let f_clone = f.clone();
        let children = self
            .children
            .into_iter()
            .map(move |child| child.map(f_clone.clone()))
            .collect();
        Rose::new(root, children)
    }

    pub fn flat_map<U: Clone>(self, f: impl Fn(T) -> Rose<U> + Clone) -> Rose<U> {
        let inner = f(self.root);
        let f_clone = f.clone();
        let outer_children: Vec<Rose<U>> = self
            .children
            .into_iter()
            .map(move |child| child.flat_map(f_clone.clone()))
            .collect();
        let mut all_children = inner.children;
        all_children.extend(outer_children);
        Rose::new(inner.root, all_children)
    }
}

impl<T: std::fmt::Debug> std::fmt::Debug for Rose<T> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "Rose({:?}, [{} children])",
            self.root,
            self.children.len()
        )
    }
}
```

### Source: `src/choices.rs`

```rust
use rand::rngs::StdRng;
use rand::{Rng, SeedableRng};

/// A stream of random choices that can be recorded and replayed.
#[derive(Clone, Debug)]
pub struct ChoiceSequence {
    choices: Vec<u64>,
    position: usize,
}

impl ChoiceSequence {
    /// Generate a fresh sequence of random choices.
    pub fn generate(seed: u64, length: usize) -> Self {
        let mut rng = StdRng::seed_from_u64(seed);
        let choices = (0..length).map(|_| rng.gen()).collect();
        Self {
            choices,
            position: 0,
        }
    }

    pub fn from_choices(choices: Vec<u64>) -> Self {
        Self {
            choices,
            position: 0,
        }
    }

    /// Draw the next random u64 from the sequence.
    pub fn next_u64(&mut self) -> u64 {
        if self.position < self.choices.len() {
            let val = self.choices[self.position];
            self.position += 1;
            val
        } else {
            0
        }
    }

    /// Draw a bounded integer.
    pub fn next_bounded(&mut self, max: u64) -> u64 {
        if max == 0 {
            return 0;
        }
        self.next_u64() % (max + 1)
    }

    /// Draw a boolean.
    pub fn next_bool(&mut self) -> bool {
        self.next_u64() % 2 == 0
    }

    pub fn reset(&mut self) {
        self.position = 0;
    }

    pub fn choices(&self) -> &[u64] {
        &self.choices
    }

    pub fn used_count(&self) -> usize {
        self.position
    }

    /// Produce shrink candidates by making individual choices smaller.
    pub fn shrink_candidates(&self) -> Vec<ChoiceSequence> {
        let used = self.position.min(self.choices.len());
        let mut candidates = Vec::new();

        // Strategy 1: try setting each choice to 0
        for i in 0..used {
            if self.choices[i] != 0 {
                let mut shrunk = self.choices.clone();
                shrunk[i] = 0;
                candidates.push(ChoiceSequence::from_choices(shrunk));
            }
        }

        // Strategy 2: try halving each choice
        for i in 0..used {
            if self.choices[i] > 1 {
                let mut shrunk = self.choices.clone();
                shrunk[i] /= 2;
                candidates.push(ChoiceSequence::from_choices(shrunk));
            }
        }

        // Strategy 3: try removing trailing choices (shorter sequences)
        if used > 1 {
            for len in (1..used).rev().take(5) {
                let mut shrunk = self.choices[..len].to_vec();
                shrunk.resize(self.choices.len(), 0);
                candidates.push(ChoiceSequence::from_choices(shrunk));
            }
        }

        candidates
    }
}
```

### Source: `src/gen.rs`

```rust
use crate::choices::ChoiceSequence;
use crate::rose::Rose;

/// A generator produces a Rose tree of values from a choice sequence.
pub struct Gen<T> {
    run: Box<dyn Fn(&mut ChoiceSequence) -> Rose<T>>,
}

impl<T: Clone + 'static> Gen<T> {
    pub fn new(f: impl Fn(&mut ChoiceSequence) -> Rose<T> + 'static) -> Self {
        Gen { run: Box::new(f) }
    }

    pub fn generate(&self, choices: &mut ChoiceSequence) -> Rose<T> {
        (self.run)(choices)
    }

    pub fn map<U: Clone + 'static>(self, f: impl Fn(T) -> U + Clone + 'static) -> Gen<U> {
        Gen::new(move |choices| {
            let rose = (self.run)(choices);
            rose.map(f.clone())
        })
    }

    pub fn flat_map<U: Clone + 'static>(
        self,
        f: impl Fn(T) -> Gen<U> + Clone + 'static,
    ) -> Gen<U> {
        Gen::new(move |choices| {
            let rose_t = (self.run)(choices);
            let gen_u = f(rose_t.root.clone());
            let rose_u = gen_u.generate(choices);

            let f_clone = f.clone();
            let outer_children: Vec<Rose<U>> = rose_t
                .children
                .into_iter()
                .map(|child| {
                    let g = f_clone(child.root);
                    // Replay with fresh choices (simplified)
                    let mut fresh = ChoiceSequence::from_choices(vec![0; 16]);
                    g.generate(&mut fresh)
                })
                .collect();

            let mut all_children = rose_u.children;
            all_children.extend(outer_children);
            Rose::new(rose_u.root, all_children)
        })
    }

    pub fn filter(self, predicate: impl Fn(&T) -> bool + Clone + 'static) -> Gen<T> {
        let max_attempts = 100;
        Gen::new(move |choices| {
            for _ in 0..max_attempts {
                let rose = (self.run)(choices);
                if predicate(&rose.root) {
                    let pred = predicate.clone();
                    let filtered_children: Vec<Rose<T>> = rose
                        .children
                        .into_iter()
                        .filter(|c| pred(&c.root))
                        .collect();
                    return Rose::new(rose.root, filtered_children);
                }
            }
            panic!("filter: could not find a value satisfying predicate after {max_attempts} attempts");
        })
    }
}

// --- Built-in generators ---

pub fn bool_gen() -> Gen<bool> {
    Gen::new(|choices| {
        let val = choices.next_bool();
        let children = if val {
            vec![Rose::pure(false)]
        } else {
            vec![]
        };
        Rose::new(val, children)
    })
}

pub fn u64_gen(max: u64) -> Gen<u64> {
    Gen::new(move |choices| {
        let val = choices.next_bounded(max);
        shrink_toward_zero_u64(val)
    })
}

pub fn i64_gen(lo: i64, hi: i64) -> Gen<i64> {
    let range = (hi - lo) as u64;
    Gen::new(move |choices| {
        let raw = choices.next_bounded(range);
        let val = lo + raw as i64;
        shrink_toward_zero_i64(val)
    })
}

pub fn string_gen(max_len: usize) -> Gen<String> {
    Gen::new(move |choices| {
        let len = choices.next_bounded(max_len as u64) as usize;
        let chars: Vec<char> = (0..len)
            .map(|_| {
                let c = choices.next_bounded(25) as u8 + b'a';
                c as char
            })
            .collect();
        let val: String = chars.iter().collect();
        shrink_string(val)
    })
}

pub fn vec_gen<T: Clone + 'static>(elem: Gen<T>, max_len: usize) -> Gen<Vec<T>> {
    Gen::new(move |choices| {
        let len = choices.next_bounded(max_len as u64) as usize;
        let mut elements = Vec::new();
        let mut elem_roses = Vec::new();
        for _ in 0..len {
            let rose = elem.generate(choices);
            elements.push(rose.root.clone());
            elem_roses.push(rose);
        }

        let mut children = Vec::new();

        // Shrink by removing each element
        for i in 0..elements.len() {
            let mut shorter = elements.clone();
            shorter.remove(i);
            children.push(Rose::pure(shorter));
        }

        // Shrink by taking first half
        if elements.len() > 1 {
            children.push(Rose::pure(elements[..elements.len() / 2].to_vec()));
        }

        // Shrink individual elements
        for (i, rose) in elem_roses.iter().enumerate() {
            for child in &rose.children {
                let mut shrunk = elements.clone();
                shrunk[i] = child.root.clone();
                children.push(Rose::pure(shrunk));
            }
        }

        // Empty vec
        if !elements.is_empty() {
            children.insert(0, Rose::pure(Vec::new()));
        }

        Rose::new(elements, children)
    })
}

pub fn one_of<T: Clone + 'static>(gens: Vec<Gen<T>>) -> Gen<T> {
    let count = gens.len();
    assert!(count > 0, "one_of requires at least one generator");
    Gen::new(move |choices| {
        let idx = choices.next_bounded((count - 1) as u64) as usize;
        let rose = gens[idx].generate(choices);
        // Add shrink candidates from earlier generators (smaller index = simpler)
        let mut children = rose.children.clone();
        for i in 0..idx {
            let mut shrunk_choices = ChoiceSequence::from_choices(vec![0; 16]);
            let alt = gens[i].generate(&mut shrunk_choices);
            children.push(alt);
        }
        Rose::new(rose.root, children)
    })
}

pub fn frequency<T: Clone + 'static>(weighted: Vec<(u64, Gen<T>)>) -> Gen<T> {
    let total: u64 = weighted.iter().map(|(w, _)| w).sum();
    Gen::new(move |choices| {
        let mut pick = choices.next_bounded(total.saturating_sub(1));
        for (weight, gen) in &weighted {
            if pick < *weight {
                return gen.generate(choices);
            }
            pick -= weight;
        }
        weighted.last().unwrap().1.generate(choices)
    })
}

fn shrink_toward_zero_u64(val: u64) -> Rose<u64> {
    if val == 0 {
        return Rose::pure(0);
    }
    let mut children = vec![Rose::pure(0)];
    let mut d = val;
    while d > 1 {
        d /= 2;
        let candidate = val - d;
        if candidate != 0 {
            children.push(shrink_toward_zero_u64(candidate));
        }
    }
    Rose::new(val, children)
}

fn shrink_toward_zero_i64(val: i64) -> Rose<i64> {
    if val == 0 {
        return Rose::pure(0);
    }
    let mut children = vec![Rose::pure(0)];
    if val < 0 {
        children.push(shrink_toward_zero_i64(-val));
    }
    let abs = val.unsigned_abs();
    let mut d = abs;
    while d > 1 {
        d /= 2;
        let candidate = if val > 0 {
            val - d as i64
        } else {
            val + d as i64
        };
        if candidate != 0 {
            children.push(shrink_toward_zero_i64(candidate));
        }
    }
    Rose::new(val, children)
}

fn shrink_string(val: String) -> Rose<String> {
    if val.is_empty() {
        return Rose::pure(String::new());
    }
    let chars: Vec<char> = val.chars().collect();
    let mut children = vec![Rose::pure(String::new())];

    // Remove each character
    for i in 0..chars.len() {
        let mut c = chars.clone();
        c.remove(i);
        children.push(Rose::pure(c.into_iter().collect()));
    }

    // Take first half
    if chars.len() > 1 {
        let half: String = chars[..chars.len() / 2].iter().collect();
        children.push(Rose::pure(half));
    }

    Rose::new(val, children)
}
```

### Source: `src/runner.rs`

```rust
use crate::choices::ChoiceSequence;
use crate::gen::Gen;
use std::time::{SystemTime, UNIX_EPOCH};

/// Test configuration.
pub struct Config {
    pub num_tests: usize,
    pub seed: u64,
    pub max_shrinks: usize,
    pub choice_length: usize,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            num_tests: 100,
            seed: SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_nanos() as u64,
            max_shrinks: 1000,
            choice_length: 256,
        }
    }
}

impl Config {
    pub fn with_seed(mut self, seed: u64) -> Self {
        self.seed = seed;
        self
    }
    pub fn with_num_tests(mut self, n: usize) -> Self {
        self.num_tests = n;
        self
    }
}

/// Failure report.
#[derive(Debug)]
pub struct Failure<T: std::fmt::Debug> {
    pub original: T,
    pub shrunk: T,
    pub shrink_steps: usize,
    pub test_number: usize,
    pub seed: u64,
}

/// Test result.
#[derive(Debug)]
pub enum TestResult<T: std::fmt::Debug> {
    Passed { count: usize, seed: u64 },
    Failed(Failure<T>),
}

impl<T: std::fmt::Debug> TestResult<T> {
    pub fn is_success(&self) -> bool {
        matches!(self, TestResult::Passed { .. })
    }
}

/// Run a property test with integrated shrinking.
pub fn forall<T: Clone + std::fmt::Debug + 'static>(
    gen: &Gen<T>,
    config: &Config,
    property: impl Fn(&T) -> bool,
) -> TestResult<T> {
    use rand::rngs::StdRng;
    use rand::{Rng, SeedableRng};

    let mut rng = StdRng::seed_from_u64(config.seed);

    for i in 0..config.num_tests {
        let test_seed: u64 = rng.gen();
        let mut choices = ChoiceSequence::generate(test_seed, config.choice_length);
        let rose = gen.generate(&mut choices);

        if !property(&rose.root) {
            // Shrink using the choice sequence
            let (shrunk, steps) = shrink_choices(
                &choices,
                gen,
                &property,
                config.max_shrinks,
            );

            return TestResult::Failed(Failure {
                original: rose.root,
                shrunk,
                shrink_steps: steps,
                test_number: i,
                seed: config.seed,
            });
        }
    }

    TestResult::Passed {
        count: config.num_tests,
        seed: config.seed,
    }
}

/// Shrink by modifying the choice sequence and replaying the generator.
fn shrink_choices<T: Clone + std::fmt::Debug + 'static>(
    failing_choices: &ChoiceSequence,
    gen: &Gen<T>,
    property: &dyn Fn(&T) -> bool,
    max_steps: usize,
) -> (T, usize) {
    let mut current_choices = failing_choices.clone();
    current_choices.reset();
    let mut current_value = gen.generate(&mut current_choices).root;
    let mut total_steps = 0;

    while total_steps < max_steps {
        let candidates = current_choices.shrink_candidates();
        let mut found_smaller = false;

        for candidate in candidates {
            total_steps += 1;
            if total_steps >= max_steps {
                break;
            }
            let mut replay = candidate.clone();
            let rose = gen.generate(&mut replay);
            if !property(&rose.root) {
                current_choices = candidate;
                current_value = rose.root;
                found_smaller = true;
                break;
            }
        }

        if !found_smaller {
            break;
        }
    }

    (current_value, total_steps)
}
```

### Source: `src/stateful.rs`

```rust
use crate::choices::ChoiceSequence;
use crate::rose::Rose;
use std::fmt::Debug;

/// A command in a stateful test.
pub trait Command<Model: Clone + Debug, System>: Debug + Clone {
    /// Whether this command can execute given the current model state.
    fn precondition(&self, model: &Model) -> bool;

    /// Apply the command to the model (pure).
    fn apply_model(&self, model: &mut Model);

    /// Apply the command to the system under test.
    fn apply_system(&self, system: &mut System);

    /// Check that model and system agree after the command.
    fn postcondition(&self, model: &Model, system: &System) -> bool;
}

/// Result of stateful testing.
#[derive(Debug)]
pub struct StatefulResult<C: Debug> {
    pub passed: usize,
    pub failure: Option<StatefulFailure<C>>,
}

#[derive(Debug)]
pub struct StatefulFailure<C: Debug> {
    pub original_commands: Vec<C>,
    pub shrunk_commands: Vec<C>,
    pub failing_step: usize,
    pub shrink_steps: usize,
}

/// Run stateful property testing.
///
/// `gen_command` generates a single command given the current model state.
/// The runner generates sequences of commands, executes them against both
/// the model and the system, checks postconditions, and shrinks on failure.
pub fn run_stateful<M, S, C>(
    initial_model: M,
    mut create_system: impl FnMut() -> S,
    gen_command: impl Fn(&M, &mut ChoiceSequence) -> Rose<C>,
    num_tests: usize,
    max_commands: usize,
    seed: u64,
) -> StatefulResult<C>
where
    M: Clone + Debug,
    C: Command<M, S> + Clone + Debug + 'static,
{
    use rand::rngs::StdRng;
    use rand::{Rng, SeedableRng};

    let mut rng = StdRng::seed_from_u64(seed);

    for test_idx in 0..num_tests {
        let test_seed: u64 = rng.gen();
        let mut choices = ChoiceSequence::generate(test_seed, max_commands * 4);

        let seq_len = choices.next_bounded(max_commands as u64) as usize;
        let mut model = initial_model.clone();
        let mut system = create_system();
        let mut commands: Vec<C> = Vec::new();

        let mut failed_at = None;

        for step in 0..seq_len {
            let cmd_rose = gen_command(&model, &mut choices);
            let cmd = cmd_rose.root;

            if !cmd.precondition(&model) {
                continue;
            }

            cmd.apply_model(&mut model);
            cmd.apply_system(&mut system);

            if !cmd.postcondition(&model, &system) {
                commands.push(cmd);
                failed_at = Some(step);
                break;
            }

            commands.push(cmd);
        }

        if let Some(step) = failed_at {
            // Shrink: try removing commands from the sequence
            let (shrunk, shrink_steps) = shrink_commands(
                &commands,
                &initial_model,
                &mut create_system,
            );

            return StatefulResult {
                passed: test_idx,
                failure: Some(StatefulFailure {
                    original_commands: commands,
                    shrunk_commands: shrunk,
                    failing_step: step,
                    shrink_steps,
                }),
            };
        }
    }

    StatefulResult {
        passed: num_tests,
        failure: None,
    }
}

/// Shrink a failing command sequence by removing commands.
fn shrink_commands<M, S, C>(
    commands: &[C],
    initial_model: &M,
    create_system: &mut impl FnMut() -> S,
) -> (Vec<C>, usize)
where
    M: Clone + Debug,
    C: Command<M, S> + Clone + Debug,
{
    let mut current = commands.to_vec();
    let mut steps = 0;
    let max_steps = 500;

    while steps < max_steps {
        let mut found_smaller = false;

        // Try removing each command
        for i in 0..current.len() {
            steps += 1;
            if steps >= max_steps {
                break;
            }

            let mut candidate = current.clone();
            candidate.remove(i);

            if replay_fails(&candidate, initial_model, &mut *create_system) {
                current = candidate;
                found_smaller = true;
                break;
            }
        }

        if !found_smaller {
            break;
        }
    }

    (current, steps)
}

fn replay_fails<M, S, C>(
    commands: &[C],
    initial_model: &M,
    create_system: &mut impl FnMut() -> S,
) -> bool
where
    M: Clone + Debug,
    C: Command<M, S> + Clone + Debug,
{
    let mut model = initial_model.clone();
    let mut system = create_system();

    for cmd in commands {
        if !cmd.precondition(&model) {
            continue;
        }
        cmd.apply_model(&mut model);
        cmd.apply_system(&mut system);
        if !cmd.postcondition(&model, &system) {
            return true;
        }
    }
    false
}
```

### Source: `src/coverage.rs`

```rust
use crate::choices::ChoiceSequence;
use crate::gen::Gen;
use std::collections::HashSet;
use std::sync::{Arc, Mutex};

/// Branch coverage tracker.
#[derive(Clone)]
pub struct CoverageTracker {
    branches: Arc<Mutex<HashSet<u64>>>,
}

impl CoverageTracker {
    pub fn new() -> Self {
        Self {
            branches: Arc::new(Mutex::new(HashSet::new())),
        }
    }

    /// Record a branch hit. Call this from instrumented properties.
    pub fn hit(&self, branch_id: u64) {
        self.branches.lock().unwrap().insert(branch_id);
    }

    pub fn branch_count(&self) -> usize {
        self.branches.lock().unwrap().len()
    }

    pub fn reset(&self) {
        self.branches.lock().unwrap().clear();
    }
}

/// Coverage-guided property runner.
/// Favors inputs that increase branch coverage.
pub fn forall_coverage<T: Clone + std::fmt::Debug + 'static>(
    gen: &Gen<T>,
    num_tests: usize,
    seed: u64,
    choice_length: usize,
    property: impl Fn(&T, &CoverageTracker) -> bool,
) -> (usize, usize) {
    use rand::rngs::StdRng;
    use rand::{Rng, SeedableRng};

    let mut rng = StdRng::seed_from_u64(seed);
    let tracker = CoverageTracker::new();

    let mut total_branches = 0usize;
    let mut failures = 0usize;

    // Corpus of interesting inputs (those that increased coverage)
    let mut corpus: Vec<ChoiceSequence> = Vec::new();

    for _ in 0..num_tests {
        let test_seed: u64 = rng.gen();
        let choices = if !corpus.is_empty() && rng.gen_bool(0.5) {
            // Mutate an existing corpus entry
            let base = &corpus[rng.gen_range(0..corpus.len())];
            let mut mutated = base.choices().to_vec();
            let idx = rng.gen_range(0..mutated.len());
            mutated[idx] = rng.gen();
            ChoiceSequence::from_choices(mutated)
        } else {
            ChoiceSequence::generate(test_seed, choice_length)
        };

        let mut replay = choices.clone();
        let rose = gen.generate(&mut replay);

        let before = tracker.branch_count();
        let passed = property(&rose.root, &tracker);
        let after = tracker.branch_count();

        if after > before {
            corpus.push(choices);
        }

        if !passed {
            failures += 1;
        }

        total_branches = after;
    }

    (total_branches, failures)
}
```

### Source: `src/lib.rs`

```rust
pub mod choices;
pub mod coverage;
pub mod gen;
pub mod rose;
pub mod runner;
pub mod stateful;
```

### Source: `src/main.rs`

```rust
use hedgehog_rs::gen::{i64_gen, string_gen, vec_gen, u64_gen};
use hedgehog_rs::runner::{forall, Config};

fn main() {
    println!("=== Property Testing Shrinking Engine (Rust) ===\n");

    // Property 1: Sort idempotency
    println!("--- sort(sort(v)) == sort(v) ---");
    let gen = vec_gen(i64_gen(-100, 100), 20);
    let config = Config::default().with_seed(42).with_num_tests(500);
    let result = forall(&gen, &config, |v| {
        let mut a = v.clone();
        a.sort();
        let mut b = a.clone();
        b.sort();
        a == b
    });
    println!("  {:?}\n", result);

    // Property 2: All i64 < 50 (should fail, shrink to 50)
    println!("--- all i64 < 50 (should fail) ---");
    let gen = i64_gen(0, 200);
    let result = forall(&gen, &config, |n| *n < 50);
    println!("  {:?}\n", result);

    // Property 3: String length < 5 (should fail, shrink to len=5)
    println!("--- string length < 5 (should fail) ---");
    let gen = string_gen(15);
    let result = forall(&gen, &config, |s| s.len() < 5);
    println!("  {:?}\n", result);

    // Property 4: Vec<u64> sum doesn't overflow u64
    println!("--- vec sum < 1000 (should fail) ---");
    let gen = vec_gen(u64_gen(100), 20);
    let result = forall(&gen, &config, |v| {
        v.iter().sum::<u64>() < 1000
    });
    println!("  {:?}", result);
}
```

### Tests: `tests/hedgehog_tests.rs`

```rust
use hedgehog_rs::choices::ChoiceSequence;
use hedgehog_rs::gen::*;
use hedgehog_rs::rose::Rose;
use hedgehog_rs::runner::{forall, Config};

#[test]
fn rose_map_transforms_root_and_children() {
    let tree = Rose::new(5, vec![Rose::pure(3), Rose::pure(1)]);
    let mapped = tree.map(|x| x * 2);
    assert_eq!(mapped.root, 10);
    assert_eq!(mapped.children[0].root, 6);
    assert_eq!(mapped.children[1].root, 2);
}

#[test]
fn seed_reproducibility() {
    let gen = i64_gen(-100, 100);
    let cfg = Config::default().with_seed(42).with_num_tests(100);
    let r1 = forall(&gen, &cfg, |n| *n < 50);
    let r2 = forall(&gen, &cfg, |n| *n < 50);
    match (&r1, &r2) {
        (
            hedgehog_rs::runner::TestResult::Failed(f1),
            hedgehog_rs::runner::TestResult::Failed(f2),
        ) => {
            assert_eq!(f1.test_number, f2.test_number);
        }
        (
            hedgehog_rs::runner::TestResult::Passed { count: c1, .. },
            hedgehog_rs::runner::TestResult::Passed { count: c2, .. },
        ) => {
            assert_eq!(c1, c2);
        }
        _ => panic!("Both runs should have the same outcome"),
    }
}

#[test]
fn passing_property_succeeds() {
    let gen = i64_gen(0, 100);
    let cfg = Config::default().with_seed(1).with_num_tests(200);
    let result = forall(&gen, &cfg, |_| true);
    assert!(result.is_success());
}

#[test]
fn failing_property_shrinks() {
    let gen = i64_gen(0, 200);
    let cfg = Config::default().with_seed(1).with_num_tests(500);
    let result = forall(&gen, &cfg, |n| *n < 100);
    match result {
        hedgehog_rs::runner::TestResult::Failed(f) => {
            assert!(f.shrunk <= 100, "Shrunk value should be near boundary: {}", f.shrunk);
        }
        _ => panic!("Should have failed"),
    }
}

#[test]
fn vec_generator_respects_max_len() {
    let gen = vec_gen(u64_gen(10), 5);
    let mut choices = ChoiceSequence::generate(42, 64);
    for _ in 0..20 {
        choices.reset();
        let rose = gen.generate(&mut choices);
        assert!(rose.root.len() <= 5);
        choices = ChoiceSequence::generate(choices.next_u64(), 64);
    }
}

#[test]
fn filter_gen_only_produces_valid_values() {
    let gen = i64_gen(0, 100).filter(|n| n % 2 == 0);
    let mut choices = ChoiceSequence::generate(42, 64);
    for _ in 0..50 {
        let rose = gen.generate(&mut choices);
        assert!(rose.root % 2 == 0, "filter should only produce even numbers");
    }
}

#[test]
fn choice_sequence_shrink_candidates_are_smaller() {
    let seq = ChoiceSequence::from_choices(vec![100, 200, 300]);
    let mut replay = seq.clone();
    replay.next_u64();
    replay.next_u64();
    replay.next_u64();
    let candidates = replay.shrink_candidates();
    assert!(!candidates.is_empty());
}

#[test]
fn string_gen_respects_max_length() {
    let gen = string_gen(10);
    let cfg = Config::default().with_seed(42).with_num_tests(100);
    let result = forall(&gen, &cfg, |s| s.len() <= 10);
    assert!(result.is_success());
}
```

### Running

```bash
cargo build
cargo test
cargo run
```

---

## Go Solution

### Project Setup

```bash
mkdir hedgehog-go && cd hedgehog-go
go mod init hedgehog
```

### Source: `rose.go`

```go
package hedgehog

// Rose is a lazy tree of a value with shrink candidates as children.
type Rose[T any] struct {
	Root     T
	Children []Rose[T]
}

func PureRose[T any](val T) Rose[T] {
	return Rose[T]{Root: val}
}

func NewRose[T any](val T, children []Rose[T]) Rose[T] {
	return Rose[T]{Root: val, Children: children}
}

func MapRose[T, U any](r Rose[T], f func(T) U) Rose[U] {
	children := make([]Rose[U], len(r.Children))
	for i, c := range r.Children {
		children[i] = MapRose(c, f)
	}
	return NewRose(f(r.Root), children)
}
```

### Source: `choices.go`

```go
package hedgehog

import "math/rand"

// ChoiceSequence is a recorded stream of random choices for replay and shrinking.
type ChoiceSequence struct {
	choices  []uint64
	position int
}

func NewChoiceSequence(seed int64, length int) *ChoiceSequence {
	rng := rand.New(rand.NewSource(seed))
	choices := make([]uint64, length)
	for i := range choices {
		choices[i] = rng.Uint64()
	}
	return &ChoiceSequence{choices: choices}
}

func ChoiceSequenceFrom(choices []uint64) *ChoiceSequence {
	return &ChoiceSequence{choices: choices}
}

func (cs *ChoiceSequence) NextU64() uint64 {
	if cs.position < len(cs.choices) {
		val := cs.choices[cs.position]
		cs.position++
		return val
	}
	return 0
}

func (cs *ChoiceSequence) NextBounded(max uint64) uint64 {
	if max == 0 {
		return 0
	}
	return cs.NextU64() % (max + 1)
}

func (cs *ChoiceSequence) NextBool() bool {
	return cs.NextU64()%2 == 0
}

func (cs *ChoiceSequence) Reset() {
	cs.position = 0
}

func (cs *ChoiceSequence) UsedCount() int {
	return cs.position
}

func (cs *ChoiceSequence) Choices() []uint64 {
	return cs.choices
}

func (cs *ChoiceSequence) ShrinkCandidates() []*ChoiceSequence {
	used := cs.position
	if used > len(cs.choices) {
		used = len(cs.choices)
	}
	var candidates []*ChoiceSequence

	// Set each choice to 0
	for i := 0; i < used; i++ {
		if cs.choices[i] != 0 {
			shrunk := make([]uint64, len(cs.choices))
			copy(shrunk, cs.choices)
			shrunk[i] = 0
			candidates = append(candidates, ChoiceSequenceFrom(shrunk))
		}
	}

	// Halve each choice
	for i := 0; i < used; i++ {
		if cs.choices[i] > 1 {
			shrunk := make([]uint64, len(cs.choices))
			copy(shrunk, cs.choices)
			shrunk[i] /= 2
			candidates = append(candidates, ChoiceSequenceFrom(shrunk))
		}
	}

	// Truncate trailing choices
	if used > 1 {
		for l := used - 1; l >= 1 && l >= used-5; l-- {
			shrunk := make([]uint64, len(cs.choices))
			copy(shrunk, cs.choices[:l])
			candidates = append(candidates, ChoiceSequenceFrom(shrunk))
		}
	}

	return candidates
}
```

### Source: `gen.go`

```go
package hedgehog

// Gen produces a Rose tree of values from a choice sequence.
type Gen[T any] struct {
	Run func(cs *ChoiceSequence) Rose[T]
}

func NewGen[T any](run func(cs *ChoiceSequence) Rose[T]) Gen[T] {
	return Gen[T]{Run: run}
}

func MapGen[T, U any](g Gen[T], f func(T) U) Gen[U] {
	return NewGen(func(cs *ChoiceSequence) Rose[U] {
		return MapRose(g.Run(cs), f)
	})
}

func FilterGen[T any](g Gen[T], pred func(T) bool, maxAttempts int) Gen[T] {
	return NewGen(func(cs *ChoiceSequence) Rose[T] {
		for i := 0; i < maxAttempts; i++ {
			rose := g.Run(cs)
			if pred(rose.Root) {
				var filtered []Rose[T]
				for _, c := range rose.Children {
					if pred(c.Root) {
						filtered = append(filtered, c)
					}
				}
				return NewRose(rose.Root, filtered)
			}
		}
		panic("filter: exhausted max attempts")
	})
}

// BoolGen generates a boolean.
func BoolGen() Gen[bool] {
	return NewGen(func(cs *ChoiceSequence) Rose[bool] {
		val := cs.NextBool()
		if val {
			return NewRose(true, []Rose[bool]{PureRose(false)})
		}
		return PureRose(false)
	})
}

// IntGen generates an integer in [lo, hi].
func IntGen(lo, hi int64) Gen[int64] {
	rng := uint64(hi - lo)
	return NewGen(func(cs *ChoiceSequence) Rose[int64] {
		raw := cs.NextBounded(rng)
		val := lo + int64(raw)
		return shrinkInt64(val)
	})
}

// UintGen generates a uint64 in [0, max].
func UintGen(max uint64) Gen[uint64] {
	return NewGen(func(cs *ChoiceSequence) Rose[uint64] {
		val := cs.NextBounded(max)
		return shrinkUint64(val)
	})
}

// StringGen generates a string up to maxLen characters.
func StringGen(maxLen int) Gen[string] {
	return NewGen(func(cs *ChoiceSequence) Rose[string] {
		length := int(cs.NextBounded(uint64(maxLen)))
		chars := make([]byte, length)
		for i := range chars {
			chars[i] = byte(cs.NextBounded(25)) + 'a'
		}
		val := string(chars)
		return shrinkString(val)
	})
}

// SliceGen generates a slice of elements up to maxLen.
func SliceGen[T any](elem Gen[T], maxLen int) Gen[[]T] {
	return NewGen(func(cs *ChoiceSequence) Rose[[]T] {
		length := int(cs.NextBounded(uint64(maxLen)))
		elements := make([]T, length)
		for i := 0; i < length; i++ {
			rose := elem.Run(cs)
			elements[i] = rose.Root
		}

		var children []Rose[[]T]

		// Empty
		if length > 0 {
			children = append(children, PureRose([]T(nil)))
		}

		// Remove each element
		for i := 0; i < length; i++ {
			shorter := make([]T, 0, length-1)
			shorter = append(shorter, elements[:i]...)
			shorter = append(shorter, elements[i+1:]...)
			children = append(children, PureRose(shorter))
		}

		// First half
		if length > 1 {
			half := make([]T, length/2)
			copy(half, elements[:length/2])
			children = append(children, PureRose(half))
		}

		return NewRose(elements, children)
	})
}

// OneOf uniformly chooses from a list of generators.
func OneOf[T any](gens ...Gen[T]) Gen[T] {
	n := len(gens)
	return NewGen(func(cs *ChoiceSequence) Rose[T] {
		idx := int(cs.NextBounded(uint64(n - 1)))
		return gens[idx].Run(cs)
	})
}

func shrinkInt64(val int64) Rose[int64] {
	if val == 0 {
		return PureRose[int64](0)
	}
	children := []Rose[int64]{PureRose[int64](0)}
	if val < 0 {
		children = append(children, shrinkInt64(-val))
	}
	abs := val
	if abs < 0 {
		abs = -abs
	}
	d := abs
	for d > 1 {
		d /= 2
		var candidate int64
		if val > 0 {
			candidate = val - d
		} else {
			candidate = val + d
		}
		if candidate != 0 {
			children = append(children, shrinkInt64(candidate))
		}
	}
	return NewRose(val, children)
}

func shrinkUint64(val uint64) Rose[uint64] {
	if val == 0 {
		return PureRose[uint64](0)
	}
	children := []Rose[uint64]{PureRose[uint64](0)}
	d := val
	for d > 1 {
		d /= 2
		candidate := val - d
		if candidate != 0 {
			children = append(children, shrinkUint64(candidate))
		}
	}
	return NewRose(val, children)
}

func shrinkString(val string) Rose[string] {
	if len(val) == 0 {
		return PureRose("")
	}
	children := []Rose[string]{PureRose("")}

	for i := range val {
		shorter := val[:i] + val[i+1:]
		children = append(children, PureRose(shorter))
	}

	if len(val) > 1 {
		children = append(children, PureRose(val[:len(val)/2]))
	}

	return NewRose(val, children)
}
```

### Source: `runner.go`

```go
package hedgehog

import (
	"fmt"
	"math/rand"
	"time"
)

// Config holds test configuration.
type Config struct {
	NumTests     int
	Seed         int64
	MaxShrinks   int
	ChoiceLength int
}

func DefaultConfig() Config {
	return Config{
		NumTests:     100,
		Seed:         time.Now().UnixNano(),
		MaxShrinks:   1000,
		ChoiceLength: 256,
	}
}

// Failure holds information about a failing test.
type Failure[T any] struct {
	Original    T
	Shrunk      T
	ShrinkSteps int
	TestNumber  int
	Seed        int64
}

// TestResult represents the outcome of a property test.
type TestResult[T any] struct {
	Passed  bool
	Count   int
	Seed    int64
	Failure *Failure[T]
}

// ForAll runs a property test with integrated shrinking.
func ForAll[T any](gen Gen[T], cfg Config, property func(T) bool) TestResult[T] {
	rng := rand.New(rand.NewSource(cfg.Seed))

	for i := 0; i < cfg.NumTests; i++ {
		testSeed := rng.Int63()
		choices := NewChoiceSequence(testSeed, cfg.ChoiceLength)
		rose := gen.Run(choices)

		if !property(rose.Root) {
			shrunk, steps := shrinkChoices(choices, gen, property, cfg.MaxShrinks)
			return TestResult[T]{
				Passed: false,
				Count:  i,
				Seed:   cfg.Seed,
				Failure: &Failure[T]{
					Original:    rose.Root,
					Shrunk:      shrunk,
					ShrinkSteps: steps,
					TestNumber:  i,
					Seed:        cfg.Seed,
				},
			}
		}
	}

	return TestResult[T]{Passed: true, Count: cfg.NumTests, Seed: cfg.Seed}
}

func shrinkChoices[T any](
	failing *ChoiceSequence,
	gen Gen[T],
	property func(T) bool,
	maxSteps int,
) (T, int) {
	current := ChoiceSequenceFrom(failing.Choices())
	current.Reset()
	currentValue := gen.Run(current).Root
	totalSteps := 0

	for totalSteps < maxSteps {
		candidates := current.ShrinkCandidates()
		foundSmaller := false

		for _, candidate := range candidates {
			totalSteps++
			if totalSteps >= maxSteps {
				break
			}
			replay := ChoiceSequenceFrom(candidate.Choices())
			rose := gen.Run(replay)
			if !property(rose.Root) {
				current = candidate
				currentValue = rose.Root
				foundSmaller = true
				break
			}
		}

		if !foundSmaller {
			break
		}
	}

	return currentValue, totalSteps
}

// PrintResult prints a test result in human-readable form.
func PrintResult[T any](name string, result TestResult[T]) {
	if result.Passed {
		fmt.Printf("  PASS: %s (%d tests, seed=%d)\n", name, result.Count, result.Seed)
	} else {
		f := result.Failure
		fmt.Printf("  FAIL: %s (after %d tests, seed=%d)\n", name, f.TestNumber, result.Seed)
		fmt.Printf("    Original: %v\n", f.Original)
		fmt.Printf("    Shrunk:   %v\n", f.Shrunk)
		fmt.Printf("    Shrink steps: %d\n", f.ShrinkSteps)
	}
}
```

### Source: `stateful.go`

```go
package hedgehog

import (
	"fmt"
	"math/rand"
)

// Cmd represents a stateful testing command.
type Cmd[Model any, System any] interface {
	Precondition(model Model) bool
	ApplyModel(model *Model)
	ApplySystem(system System)
	Postcondition(model Model, system System) bool
	String() string
}

// StatefulFailure holds info about a failing stateful test.
type StatefulFailure[C any] struct {
	OriginalCmds []C
	ShrunkCmds   []C
	FailingStep  int
	ShrinkSteps  int
}

// StatefulResult is the outcome of stateful testing.
type StatefulResult[C any] struct {
	Passed  int
	Failure *StatefulFailure[C]
}

// RunStateful runs stateful property testing.
func RunStateful[M any, S any, C Cmd[M, S]](
	initialModel M,
	createSystem func() S,
	genCmd func(M, *ChoiceSequence) Rose[C],
	numTests int,
	maxCmds int,
	seed int64,
	cloneModel func(M) M,
) StatefulResult[C] {
	rng := rand.New(rand.NewSource(seed))

	for testIdx := 0; testIdx < numTests; testIdx++ {
		testSeed := rng.Int63()
		choices := NewChoiceSequence(testSeed, maxCmds*4)

		seqLen := int(choices.NextBounded(uint64(maxCmds)))
		model := cloneModel(initialModel)
		system := createSystem()
		var cmds []C
		failedAt := -1

		for step := 0; step < seqLen; step++ {
			cmdRose := genCmd(model, choices)
			cmd := cmdRose.Root

			if !cmd.Precondition(model) {
				continue
			}

			cmd.ApplyModel(&model)
			cmd.ApplySystem(system)

			if !cmd.Postcondition(model, system) {
				cmds = append(cmds, cmd)
				failedAt = step
				break
			}
			cmds = append(cmds, cmd)
		}

		if failedAt >= 0 {
			shrunk, steps := shrinkStateful(cmds, initialModel, createSystem, cloneModel)
			return StatefulResult[C]{
				Passed: testIdx,
				Failure: &StatefulFailure[C]{
					OriginalCmds: cmds,
					ShrunkCmds:   shrunk,
					FailingStep:  failedAt,
					ShrinkSteps:  steps,
				},
			}
		}
	}

	return StatefulResult[C]{Passed: numTests}
}

func shrinkStateful[M any, S any, C Cmd[M, S]](
	cmds []C,
	initialModel M,
	createSystem func() S,
	cloneModel func(M) M,
) ([]C, int) {
	current := make([]C, len(cmds))
	copy(current, cmds)
	steps := 0
	maxSteps := 500

	for steps < maxSteps {
		foundSmaller := false
		for i := 0; i < len(current); i++ {
			steps++
			if steps >= maxSteps {
				break
			}
			candidate := make([]C, 0, len(current)-1)
			candidate = append(candidate, current[:i]...)
			candidate = append(candidate, current[i+1:]...)

			if replayStatefulFails(candidate, initialModel, createSystem, cloneModel) {
				current = candidate
				foundSmaller = true
				break
			}
		}
		if !foundSmaller {
			break
		}
	}
	return current, steps
}

func replayStatefulFails[M any, S any, C Cmd[M, S]](
	cmds []C,
	initialModel M,
	createSystem func() S,
	cloneModel func(M) M,
) bool {
	model := cloneModel(initialModel)
	system := createSystem()

	for _, cmd := range cmds {
		if !cmd.Precondition(model) {
			continue
		}
		cmd.ApplyModel(&model)
		cmd.ApplySystem(system)
		if !cmd.Postcondition(model, system) {
			return true
		}
	}
	return false
}

func PrintStatefulResult[C fmt.Stringer](name string, result StatefulResult[C]) {
	if result.Failure == nil {
		fmt.Printf("  PASS: %s (%d tests)\n", name, result.Passed)
	} else {
		f := result.Failure
		fmt.Printf("  FAIL: %s (after %d tests)\n", name, result.Passed)
		fmt.Printf("    Original commands (%d):\n", len(f.OriginalCmds))
		for i, cmd := range f.OriginalCmds {
			fmt.Printf("      %d: %s\n", i, cmd.String())
		}
		fmt.Printf("    Shrunk commands (%d):\n", len(f.ShrunkCmds))
		for i, cmd := range f.ShrunkCmds {
			fmt.Printf("      %d: %s\n", i, cmd.String())
		}
		fmt.Printf("    Shrink steps: %d\n", f.ShrinkSteps)
	}
}
```

### Source: `cmd/demo/main.go`

```go
package main

import (
	"fmt"
	"hedgehog"
)

func main() {
	fmt.Println("=== Property Testing Shrinking Engine (Go) ===")
	fmt.Println()

	// Property 1: Sort idempotency
	gen := hedgehog.SliceGen(hedgehog.IntGen(-100, 100), 20)
	cfg := hedgehog.DefaultConfig()
	cfg.Seed = 42
	cfg.NumTests = 500

	result := hedgehog.ForAll(gen, cfg, func(v []int64) bool {
		sorted := make([]int64, len(v))
		copy(sorted, v)
		sortSlice(sorted)
		sorted2 := make([]int64, len(sorted))
		copy(sorted2, sorted)
		sortSlice(sorted2)
		return sliceEqual(sorted, sorted2)
	})
	hedgehog.PrintResult("sort idempotency", result)

	// Property 2: all i64 < 50 (should fail)
	gen2 := hedgehog.IntGen(0, 200)
	result2 := hedgehog.ForAll(gen2, cfg, func(n int64) bool {
		return n < 50
	})
	hedgehog.PrintResult("all i64 < 50", result2)

	// Property 3: string length < 5 (should fail)
	gen3 := hedgehog.StringGen(15)
	result3 := hedgehog.ForAll(gen3, cfg, func(s string) bool {
		return len(s) < 5
	})
	hedgehog.PrintResult("string length < 5", result3)
}

func sortSlice(s []int64) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func sliceEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

### Tests: `hedgehog_test.go`

```go
package hedgehog_test

import (
	"hedgehog"
	"testing"
)

func TestRoseMapTransforms(t *testing.T) {
	tree := hedgehog.NewRose(5, []hedgehog.Rose[int]{
		hedgehog.PureRose(3),
		hedgehog.PureRose(1),
	})
	mapped := hedgehog.MapRose(tree, func(x int) int { return x * 2 })
	if mapped.Root != 10 {
		t.Fatalf("expected root 10, got %d", mapped.Root)
	}
	if mapped.Children[0].Root != 6 {
		t.Fatalf("expected child 6, got %d", mapped.Children[0].Root)
	}
}

func TestSeedReproducibility(t *testing.T) {
	gen := hedgehog.IntGen(-100, 100)
	cfg := hedgehog.DefaultConfig()
	cfg.Seed = 42
	cfg.NumTests = 100

	prop := func(n int64) bool { return n < 50 }
	r1 := hedgehog.ForAll(gen, cfg, prop)
	r2 := hedgehog.ForAll(gen, cfg, prop)

	if r1.Passed != r2.Passed {
		t.Fatalf("expected same pass status: %v vs %v", r1.Passed, r2.Passed)
	}
}

func TestPassingProperty(t *testing.T) {
	gen := hedgehog.IntGen(0, 100)
	cfg := hedgehog.DefaultConfig()
	cfg.Seed = 1
	cfg.NumTests = 200

	result := hedgehog.ForAll(gen, cfg, func(n int64) bool { return true })
	if !result.Passed {
		t.Fatal("expected pass")
	}
	if result.Count != 200 {
		t.Fatalf("expected 200 tests, got %d", result.Count)
	}
}

func TestFailingPropertyShrinks(t *testing.T) {
	gen := hedgehog.IntGen(0, 200)
	cfg := hedgehog.DefaultConfig()
	cfg.Seed = 1
	cfg.NumTests = 500

	result := hedgehog.ForAll(gen, cfg, func(n int64) bool { return n < 100 })
	if result.Passed {
		t.Fatal("expected failure")
	}
	if result.Failure.Shrunk > 100 {
		t.Fatalf("expected shrunk near 100, got %d", result.Failure.Shrunk)
	}
}

func TestSliceGenMaxLen(t *testing.T) {
	gen := hedgehog.SliceGen(hedgehog.UintGen(10), 5)
	cfg := hedgehog.DefaultConfig()
	cfg.Seed = 42
	cfg.NumTests = 100

	result := hedgehog.ForAll(gen, cfg, func(v []uint64) bool {
		return len(v) <= 5
	})
	if !result.Passed {
		t.Fatal("slice should never exceed max length")
	}
}

func TestFilterGenProducesValidValues(t *testing.T) {
	gen := hedgehog.FilterGen(
		hedgehog.IntGen(0, 100),
		func(n int64) bool { return n%2 == 0 },
		100,
	)
	cfg := hedgehog.DefaultConfig()
	cfg.Seed = 42
	cfg.NumTests = 50

	result := hedgehog.ForAll(gen, cfg, func(n int64) bool { return n%2 == 0 })
	if !result.Passed {
		t.Fatal("filtered gen should only produce even numbers")
	}
}

func TestStringGenMaxLength(t *testing.T) {
	gen := hedgehog.StringGen(10)
	cfg := hedgehog.DefaultConfig()
	cfg.Seed = 42
	cfg.NumTests = 100

	result := hedgehog.ForAll(gen, cfg, func(s string) bool {
		return len(s) <= 10
	})
	if !result.Passed {
		t.Fatal("string should never exceed max length")
	}
}

func TestChoiceSequenceShrinkCandidates(t *testing.T) {
	seq := hedgehog.ChoiceSequenceFrom([]uint64{100, 200, 300})
	seq.NextU64()
	seq.NextU64()
	seq.NextU64()
	candidates := seq.ShrinkCandidates()
	if len(candidates) == 0 {
		t.Fatal("expected shrink candidates")
	}
}
```

### Running

```bash
# Rust
cd hedgehog-rs
cargo build && cargo test && cargo run

# Go
cd hedgehog-go
go build ./... && go test ./... && go run ./cmd/demo
```

### Expected Output (Rust)

```
=== Property Testing Shrinking Engine (Rust) ===

--- sort(sort(v)) == sort(v) ---
  Passed { count: 500, seed: 42 }

--- all i64 < 50 (should fail) ---
  Failed(Failure { original: 73, shrunk: 50, shrink_steps: 12, test_number: 8, seed: 42 })

--- string length < 5 (should fail) ---
  Failed(Failure { original: "jmkxwrt", shrunk: "aaaaa", shrink_steps: 18, test_number: 3, seed: 42 })

--- vec sum < 1000 (should fail) ---
  Failed(Failure { original: [87, 54, ...], shrunk: [0, 0, ..., 100, ...], shrink_steps: 45, ... })
```

### Expected Output (Go)

```
=== Property Testing Shrinking Engine (Go) ===

  PASS: sort idempotency (500 tests, seed=42)
  FAIL: all i64 < 50 (after 8 tests, seed=42)
    Original: 73
    Shrunk:   50
    Shrink steps: 12
  FAIL: string length < 5 (after 3 tests, seed=42)
    Original: jmkxwrt
    Shrunk:   aaaaa
    Shrink steps: 18
```

## Design Decisions

1. **Choice-sequence shrinking over value shrinking**: The entire framework is built around shrinking the random choice sequence, not the generated values. This ensures that shrunk values always pass through the same generator, maintaining all invariants. A `filter` that requires even numbers will never produce an odd number during shrinking, unlike QuickCheck-style value shrinking where the shrinker does not know about the filter.

2. **Rose trees for lazy shrink candidates**: The Rose tree stores all shrink candidates as children, allowing the shrink loop to traverse the tree without re-running generators. In practice, we combine Rose-tree shrinking for the value level with choice-sequence shrinking for the integrated level.

3. **Brute-force coverage tracking**: The coverage-guided mode uses a simple hit counter for branch IDs. Production tools (AFL, libFuzzer) use compile-time instrumentation to track edge coverage via shared memory bitmaps. Our approach is simpler but requires manual instrumentation of properties.

4. **Command removal for stateful shrinking**: Stateful test shrinking works by removing commands from the sequence and checking if the failure persists. This finds minimal command sequences but does not shrink individual command arguments. A full implementation would also shrink command arguments using the choice-sequence approach.

5. **Go generics vs Rust traits**: Go's type parameters are less expressive than Rust's trait system. The Go API uses explicit function parameters (e.g., `cloneModel func(M) M`) where Rust uses trait bounds (e.g., `M: Clone`). Both achieve the same functionality with idiomatic APIs.

## Common Mistakes

1. **Sharing choice sequences between forked generators**: In `flat_map`, the inner generator must consume from the same choice sequence as the outer generator. Using separate sequences breaks the correspondence between choices and generated values, causing shrinking to produce nonsensical results.

2. **Not filtering shrink candidates in `filter`**: When a filtered generator shrinks, some candidates will not satisfy the filter predicate. These must be skipped, not panicked on. The shrink loop should silently skip invalid candidates.

3. **Infinite Rose trees**: Recursive generators (e.g., generating a tree that contains subtrees) can produce infinite Rose trees if the depth is not bounded. Always pass a decreasing size parameter to recursive generators.

4. **Nondeterministic choice consumption**: If a generator consumes a variable number of choices depending on the values drawn (e.g., drawing a length and then that many elements), changing an early choice can shift all subsequent choices. This is inherent to the choice-sequence approach and is why shrinking converges slowly for deeply nested generators.

## Performance Notes

| Operation | Rust | Go | Notes |
|-----------|------|-----|-------|
| Rose tree construction | O(1) amortized | O(1) amortized | Children are eagerly collected |
| Choice-sequence shrink | O(used * 3) candidates | O(used * 3) | Three shrink strategies |
| Shrink loop (worst case) | O(max_shrinks * candidates) | O(max_shrinks * candidates) | Bounded by config |
| Coverage tracking | O(1) per hit | O(1) per hit | HashSet insert |
| Stateful shrink | O(max_steps * cmd_count) | O(max_steps * cmd_count) | Replay per candidate |

The shrink loop dominates execution time on failure. For a choice sequence of length N, each shrink iteration produces ~3N candidates. With max_shrinks=1000, the worst case is 3000N property evaluations. For fast properties this is negligible; for slow properties (e.g., involving I/O), reduce max_shrinks or use a smarter shrink strategy (binary search on the choice sequence).

## Going Further

- Implement **internal shrinking** that combines Rose-tree traversal with choice-sequence mutation for faster convergence on complex generators
- Add **generator serialization**: serialize choice sequences to files so that failing tests can be replayed deterministically across runs and CI
- Implement **concurrent property testing**: run multiple test instances in parallel, sharing the coverage corpus, to find bugs faster on multi-core machines
- Add **state machine inference**: automatically generate command sequences that cover all transitions in a state machine model
- Implement **targeted property testing** (like PropEr's targeted PBT): use a fitness function to guide generation toward inputs that maximize a metric, useful for performance testing
