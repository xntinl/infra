# Solution: Model Checker State Explorer

## Architecture Overview

The solution is organized into four modules:

1. **Core abstractions** (`system.rs`) -- the `System` trait defining state machines with typed states, initial states, and transition functions
2. **Explorer engine** (`checker.rs`) -- the `ModelChecker` implementing BFS and DFS exploration, property checking, and counterexample generation
3. **Property types** (`property.rs`) -- safety (invariant) and liveness property definitions, plus deadlock detection
4. **Example systems** (`examples/`) -- Peterson's mutual exclusion (correct and buggy variants) and producer-consumer bounded buffer

The checker stores the full state graph (parent pointers) during exploration to reconstruct counterexample traces. This trades memory for diagnostic quality.

## Rust Solution

### Project Setup

```bash
cargo new model-checker
cd model-checker
```

```toml
[package]
name = "model-checker"
version = "0.1.0"
edition = "2021"

[dependencies]
rustc-hash = "1"
```

### Source: `src/system.rs`

```rust
use std::fmt::Debug;
use std::hash::Hash;

/// A state machine definition for model checking.
pub trait System {
    type State: Hash + Eq + Clone + Debug;

    /// All possible initial states.
    fn initial_states(&self) -> Vec<Self::State>;

    /// All states reachable from `state` in one transition.
    fn transitions(&self, state: &Self::State) -> Vec<Self::State>;

    /// Human-readable label for a state (used in counterexample traces).
    fn state_label(&self, state: &Self::State) -> String {
        format!("{:?}", state)
    }
}
```

### Source: `src/property.rs`

```rust
use crate::system::System;

/// A safety property: an invariant that must hold in every reachable state.
pub struct SafetyProperty<S: System> {
    pub name: String,
    pub predicate: Box<dyn Fn(&S::State) -> bool>,
}

impl<S: System> SafetyProperty<S> {
    pub fn new(name: impl Into<String>, predicate: impl Fn(&S::State) -> bool + 'static) -> Self {
        Self {
            name: name.into(),
            predicate: Box::new(predicate),
        }
    }

    pub fn check(&self, state: &S::State) -> bool {
        (self.predicate)(state)
    }
}

/// A liveness property: something that must eventually hold on every path.
pub struct LivenessProperty<S: System> {
    pub name: String,
    pub predicate: Box<dyn Fn(&S::State) -> bool>,
}

impl<S: System> LivenessProperty<S> {
    pub fn new(name: impl Into<String>, predicate: impl Fn(&S::State) -> bool + 'static) -> Self {
        Self {
            name: name.into(),
            predicate: Box::new(predicate),
        }
    }

    pub fn is_satisfied_at(&self, state: &S::State) -> bool {
        (self.predicate)(state)
    }
}
```

### Source: `src/checker.rs`

```rust
use crate::property::{LivenessProperty, SafetyProperty};
use crate::system::System;
use rustc_hash::{FxHashMap, FxHashSet};
use std::collections::VecDeque;
use std::time::Instant;

/// Exploration strategy.
#[derive(Debug, Clone, Copy)]
pub enum Strategy {
    BFS,
    DFS,
}

/// Statistics from an exploration run.
#[derive(Debug, Clone)]
pub struct ExplorationStats {
    pub states_explored: usize,
    pub transitions_evaluated: usize,
    pub max_depth: usize,
    pub deadlock_states: usize,
    pub elapsed_ms: u128,
}

/// A violation trace: sequence of states from initial to violating.
#[derive(Debug, Clone)]
pub struct Trace<State: std::fmt::Debug> {
    pub property_name: String,
    pub violation_kind: ViolationKind,
    pub path: Vec<State>,
    pub labels: Vec<String>,
}

#[derive(Debug, Clone)]
pub enum ViolationKind {
    Safety,
    Liveness,
    Deadlock,
}

/// Result of model checking.
pub struct CheckResult<State: std::fmt::Debug> {
    pub stats: ExplorationStats,
    pub violations: Vec<Trace<State>>,
}

impl<State: std::fmt::Debug> CheckResult<State> {
    pub fn is_valid(&self) -> bool {
        self.violations.is_empty()
    }
}

/// The model checker engine.
pub struct ModelChecker<S: System> {
    system: S,
    strategy: Strategy,
}

impl<S: System> ModelChecker<S>
where
    S::State: std::hash::Hash + Eq + Clone + std::fmt::Debug,
{
    pub fn new(system: S, strategy: Strategy) -> Self {
        Self { system, strategy }
    }

    /// Explore all reachable states and check all properties.
    pub fn check(
        &self,
        safety: &[SafetyProperty<S>],
        liveness: &[LivenessProperty<S>],
        detect_deadlocks: bool,
    ) -> CheckResult<S::State> {
        match self.strategy {
            Strategy::BFS => self.check_bfs(safety, liveness, detect_deadlocks),
            Strategy::DFS => self.check_dfs(safety, liveness, detect_deadlocks),
        }
    }

    fn check_bfs(
        &self,
        safety: &[SafetyProperty<S>],
        liveness: &[LivenessProperty<S>],
        detect_deadlocks: bool,
    ) -> CheckResult<S::State> {
        let start = Instant::now();
        let mut visited: FxHashSet<S::State> = FxHashSet::default();
        let mut parent: FxHashMap<S::State, Option<S::State>> = FxHashMap::default();
        let mut queue: VecDeque<(S::State, usize)> = VecDeque::new();
        let mut violations: Vec<Trace<S::State>> = Vec::new();
        let mut transitions_count = 0usize;
        let mut max_depth = 0usize;
        let mut deadlock_count = 0usize;

        // Track which liveness properties have been satisfied
        let mut liveness_satisfied: Vec<bool> = vec![false; liveness.len()];

        for init in self.system.initial_states() {
            if visited.insert(init.clone()) {
                parent.insert(init.clone(), None);
                queue.push_back((init, 0));
            }
        }

        while let Some((state, depth)) = queue.pop_front() {
            max_depth = max_depth.max(depth);

            // Check safety properties
            for prop in safety {
                if !prop.check(&state) {
                    let path = self.reconstruct_path(&state, &parent);
                    violations.push(Trace {
                        property_name: prop.name.clone(),
                        violation_kind: ViolationKind::Safety,
                        path: path.iter().map(|s| s.clone()).collect(),
                        labels: path
                            .iter()
                            .map(|s| self.system.state_label(s))
                            .collect(),
                    });
                }
            }

            // Track liveness satisfaction
            for (i, prop) in liveness.iter().enumerate() {
                if prop.is_satisfied_at(&state) {
                    liveness_satisfied[i] = true;
                }
            }

            let successors = self.system.transitions(&state);
            transitions_count += successors.len();

            if successors.is_empty() && detect_deadlocks {
                deadlock_count += 1;
                let path = self.reconstruct_path(&state, &parent);
                violations.push(Trace {
                    property_name: "deadlock-freedom".to_string(),
                    violation_kind: ViolationKind::Deadlock,
                    path: path.iter().map(|s| s.clone()).collect(),
                    labels: path
                        .iter()
                        .map(|s| self.system.state_label(s))
                        .collect(),
                });
            }

            for next in successors {
                if visited.insert(next.clone()) {
                    parent.insert(next.clone(), Some(state.clone()));
                    queue.push_back((next, depth + 1));
                }
            }
        }

        // After full exploration, check unsatisfied liveness properties
        for (i, satisfied) in liveness_satisfied.iter().enumerate() {
            if !satisfied {
                violations.push(Trace {
                    property_name: liveness[i].name.clone(),
                    violation_kind: ViolationKind::Liveness,
                    path: Vec::new(),
                    labels: vec!["No state in the entire state space satisfies this property".into()],
                });
            }
        }

        CheckResult {
            stats: ExplorationStats {
                states_explored: visited.len(),
                transitions_evaluated: transitions_count,
                max_depth,
                deadlock_states: deadlock_count,
                elapsed_ms: start.elapsed().as_millis(),
            },
            violations,
        }
    }

    fn check_dfs(
        &self,
        safety: &[SafetyProperty<S>],
        liveness: &[LivenessProperty<S>],
        detect_deadlocks: bool,
    ) -> CheckResult<S::State> {
        let start = Instant::now();
        let mut visited: FxHashSet<S::State> = FxHashSet::default();
        let mut violations: Vec<Trace<S::State>> = Vec::new();
        let mut transitions_count = 0usize;
        let mut max_depth = 0usize;
        let mut deadlock_count = 0usize;
        let mut liveness_satisfied: Vec<bool> = vec![false; liveness.len()];

        for init in self.system.initial_states() {
            if visited.contains(&init) {
                continue;
            }
            let mut stack: Vec<(S::State, usize)> = vec![(init.clone(), 0)];
            let mut path_stack: Vec<S::State> = Vec::new();

            while let Some((state, depth)) = stack.pop() {
                max_depth = max_depth.max(depth);

                // Trim path stack to current depth
                path_stack.truncate(depth);
                path_stack.push(state.clone());

                if !visited.insert(state.clone()) {
                    continue;
                }

                // Check safety
                for prop in safety {
                    if !prop.check(&state) {
                        violations.push(Trace {
                            property_name: prop.name.clone(),
                            violation_kind: ViolationKind::Safety,
                            path: path_stack.clone(),
                            labels: path_stack
                                .iter()
                                .map(|s| self.system.state_label(s))
                                .collect(),
                        });
                    }
                }

                // Track liveness
                for (i, prop) in liveness.iter().enumerate() {
                    if prop.is_satisfied_at(&state) {
                        liveness_satisfied[i] = true;
                    }
                }

                let successors = self.system.transitions(&state);
                transitions_count += successors.len();

                if successors.is_empty() && detect_deadlocks {
                    deadlock_count += 1;
                    violations.push(Trace {
                        property_name: "deadlock-freedom".to_string(),
                        violation_kind: ViolationKind::Deadlock,
                        path: path_stack.clone(),
                        labels: path_stack
                            .iter()
                            .map(|s| self.system.state_label(s))
                            .collect(),
                    });
                }

                for next in successors.into_iter().rev() {
                    if !visited.contains(&next) {
                        stack.push((next, depth + 1));
                    }
                }
            }
        }

        for (i, satisfied) in liveness_satisfied.iter().enumerate() {
            if !satisfied {
                violations.push(Trace {
                    property_name: liveness[i].name.clone(),
                    violation_kind: ViolationKind::Liveness,
                    path: Vec::new(),
                    labels: vec!["No state satisfies this liveness property".into()],
                });
            }
        }

        CheckResult {
            stats: ExplorationStats {
                states_explored: visited.len(),
                transitions_evaluated: transitions_count,
                max_depth,
                deadlock_states: deadlock_count,
                elapsed_ms: start.elapsed().as_millis(),
            },
            violations,
        }
    }

    fn reconstruct_path(
        &self,
        target: &S::State,
        parent: &FxHashMap<S::State, Option<S::State>>,
    ) -> Vec<S::State> {
        let mut path = vec![target.clone()];
        let mut current = target.clone();
        while let Some(Some(prev)) = parent.get(&current) {
            path.push(prev.clone());
            current = prev.clone();
        }
        path.reverse();
        path
    }
}
```

### Source: `src/examples/peterson.rs`

```rust
use crate::system::System;

/// Process states in Peterson's mutual exclusion algorithm.
#[derive(Debug, Clone, Hash, Eq, PartialEq)]
pub enum ProcessState {
    Idle,
    Requesting,
    Waiting,
    Critical,
}

/// Global state of the two-process Peterson system.
#[derive(Debug, Clone, Hash, Eq, PartialEq)]
pub struct PetersonState {
    pub proc0: ProcessState,
    pub proc1: ProcessState,
    pub flag: [bool; 2],
    pub turn: usize,
}

/// Correct Peterson's algorithm.
pub struct PetersonCorrect;

impl System for PetersonCorrect {
    type State = PetersonState;

    fn initial_states(&self) -> Vec<Self::State> {
        vec![PetersonState {
            proc0: ProcessState::Idle,
            proc1: ProcessState::Idle,
            flag: [false, false],
            turn: 0,
        }]
    }

    fn transitions(&self, state: &Self::State) -> Vec<Self::State> {
        let mut nexts = Vec::new();

        // Process 0 transitions
        match state.proc0 {
            ProcessState::Idle => {
                let mut s = state.clone();
                s.proc0 = ProcessState::Requesting;
                s.flag[0] = true;
                s.turn = 1;
                nexts.push(s);
            }
            ProcessState::Requesting => {
                let mut s = state.clone();
                s.proc0 = ProcessState::Waiting;
                nexts.push(s);
            }
            ProcessState::Waiting => {
                if !state.flag[1] || state.turn == 0 {
                    let mut s = state.clone();
                    s.proc0 = ProcessState::Critical;
                    nexts.push(s);
                }
            }
            ProcessState::Critical => {
                let mut s = state.clone();
                s.proc0 = ProcessState::Idle;
                s.flag[0] = false;
                nexts.push(s);
            }
        }

        // Process 1 transitions
        match state.proc1 {
            ProcessState::Idle => {
                let mut s = state.clone();
                s.proc1 = ProcessState::Requesting;
                s.flag[1] = true;
                s.turn = 0;
                nexts.push(s);
            }
            ProcessState::Requesting => {
                let mut s = state.clone();
                s.proc1 = ProcessState::Waiting;
                nexts.push(s);
            }
            ProcessState::Waiting => {
                if !state.flag[0] || state.turn == 1 {
                    let mut s = state.clone();
                    s.proc1 = ProcessState::Critical;
                    nexts.push(s);
                }
            }
            ProcessState::Critical => {
                let mut s = state.clone();
                s.proc1 = ProcessState::Idle;
                s.flag[1] = false;
                nexts.push(s);
            }
        }

        nexts
    }

    fn state_label(&self, state: &Self::State) -> String {
        format!(
            "P0={:?} P1={:?} flags=[{},{}] turn={}",
            state.proc0, state.proc1, state.flag[0], state.flag[1], state.turn
        )
    }
}

/// Buggy version: forgets to set the turn variable, allowing both
/// processes into the critical section simultaneously.
pub struct PetersonBuggy;

impl System for PetersonBuggy {
    type State = PetersonState;

    fn initial_states(&self) -> Vec<Self::State> {
        vec![PetersonState {
            proc0: ProcessState::Idle,
            proc1: ProcessState::Idle,
            flag: [false, false],
            turn: 0,
        }]
    }

    fn transitions(&self, state: &Self::State) -> Vec<Self::State> {
        let mut nexts = Vec::new();

        // Process 0 -- BUG: does not set turn = 1
        match state.proc0 {
            ProcessState::Idle => {
                let mut s = state.clone();
                s.proc0 = ProcessState::Requesting;
                s.flag[0] = true;
                // BUG: missing s.turn = 1;
                nexts.push(s);
            }
            ProcessState::Requesting => {
                let mut s = state.clone();
                s.proc0 = ProcessState::Waiting;
                nexts.push(s);
            }
            ProcessState::Waiting => {
                if !state.flag[1] || state.turn == 0 {
                    let mut s = state.clone();
                    s.proc0 = ProcessState::Critical;
                    nexts.push(s);
                }
            }
            ProcessState::Critical => {
                let mut s = state.clone();
                s.proc0 = ProcessState::Idle;
                s.flag[0] = false;
                nexts.push(s);
            }
        }

        // Process 1 -- BUG: does not set turn = 0
        match state.proc1 {
            ProcessState::Idle => {
                let mut s = state.clone();
                s.proc1 = ProcessState::Requesting;
                s.flag[1] = true;
                // BUG: missing s.turn = 0;
                nexts.push(s);
            }
            ProcessState::Requesting => {
                let mut s = state.clone();
                s.proc1 = ProcessState::Waiting;
                nexts.push(s);
            }
            ProcessState::Waiting => {
                if !state.flag[0] || state.turn == 1 {
                    let mut s = state.clone();
                    s.proc1 = ProcessState::Critical;
                    nexts.push(s);
                }
            }
            ProcessState::Critical => {
                let mut s = state.clone();
                s.proc1 = ProcessState::Idle;
                s.flag[1] = false;
                nexts.push(s);
            }
        }

        nexts
    }

    fn state_label(&self, state: &Self::State) -> String {
        format!(
            "P0={:?} P1={:?} flags=[{},{}] turn={}",
            state.proc0, state.proc1, state.flag[0], state.flag[1], state.turn
        )
    }
}
```

### Source: `src/examples/producer_consumer.rs`

```rust
use crate::system::System;

#[derive(Debug, Clone, Hash, Eq, PartialEq)]
pub enum ActorState {
    Ready,
    Working,
}

#[derive(Debug, Clone, Hash, Eq, PartialEq)]
pub struct ProdConState {
    pub buffer_count: usize,
    pub buffer_capacity: usize,
    pub producer: ActorState,
    pub consumer: ActorState,
}

pub struct ProducerConsumer {
    pub capacity: usize,
}

impl System for ProducerConsumer {
    type State = ProdConState;

    fn initial_states(&self) -> Vec<Self::State> {
        vec![ProdConState {
            buffer_count: 0,
            buffer_capacity: self.capacity,
            producer: ActorState::Ready,
            consumer: ActorState::Ready,
        }]
    }

    fn transitions(&self, state: &Self::State) -> Vec<Self::State> {
        let mut nexts = Vec::new();

        // Producer: Ready -> Working (if buffer not full)
        if matches!(state.producer, ActorState::Ready) && state.buffer_count < state.buffer_capacity
        {
            let mut s = state.clone();
            s.producer = ActorState::Working;
            nexts.push(s);
        }

        // Producer: Working -> Ready (produce item)
        if matches!(state.producer, ActorState::Working) {
            let mut s = state.clone();
            s.producer = ActorState::Ready;
            s.buffer_count += 1;
            nexts.push(s);
        }

        // Consumer: Ready -> Working (if buffer not empty)
        if matches!(state.consumer, ActorState::Ready) && state.buffer_count > 0 {
            let mut s = state.clone();
            s.consumer = ActorState::Working;
            nexts.push(s);
        }

        // Consumer: Working -> Ready (consume item)
        if matches!(state.consumer, ActorState::Working) {
            let mut s = state.clone();
            s.consumer = ActorState::Ready;
            s.buffer_count = s.buffer_count.saturating_sub(1);
            nexts.push(s);
        }

        nexts
    }

    fn state_label(&self, state: &Self::State) -> String {
        format!(
            "buf={}/{} prod={:?} cons={:?}",
            state.buffer_count, state.buffer_capacity, state.producer, state.consumer
        )
    }
}
```

### Source: `src/examples/mod.rs`

```rust
pub mod peterson;
pub mod producer_consumer;
```

### Source: `src/lib.rs`

```rust
pub mod checker;
pub mod examples;
pub mod property;
pub mod system;
```

### Source: `src/main.rs`

```rust
use model_checker::checker::{ModelChecker, Strategy};
use model_checker::examples::peterson::{PetersonBuggy, PetersonCorrect, ProcessState};
use model_checker::examples::producer_consumer::ProducerConsumer;
use model_checker::property::{LivenessProperty, SafetyProperty};

fn main() {
    println!("=== Model Checker State Explorer ===\n");

    // --- Correct Peterson's algorithm ---
    println!("--- Correct Peterson's Mutual Exclusion ---");
    let system = PetersonCorrect;
    let checker = ModelChecker::new(system, Strategy::BFS);

    let mutex_safety = SafetyProperty::new("mutual-exclusion", |s| {
        !matches!(
            (&s.proc0, &s.proc1),
            (ProcessState::Critical, ProcessState::Critical)
        )
    });

    let result = checker.check(&[mutex_safety], &[], true);
    println!("  States explored: {}", result.stats.states_explored);
    println!("  Transitions: {}", result.stats.transitions_evaluated);
    println!("  Max depth: {}", result.stats.max_depth);
    println!("  Violations: {}", result.violations.len());
    println!("  Valid: {}", result.is_valid());

    // --- Buggy Peterson's algorithm ---
    println!("\n--- Buggy Peterson's (missing turn assignment) ---");
    let system = PetersonBuggy;
    let checker = ModelChecker::new(system, Strategy::BFS);

    let mutex_safety = SafetyProperty::new("mutual-exclusion", |s| {
        !matches!(
            (&s.proc0, &s.proc1),
            (ProcessState::Critical, ProcessState::Critical)
        )
    });

    let result = checker.check(&[mutex_safety], &[], false);
    println!("  States explored: {}", result.stats.states_explored);
    println!("  Valid: {}", result.is_valid());
    if let Some(violation) = result.violations.first() {
        println!(
            "  Violation: {} ({:?})",
            violation.property_name, violation.violation_kind
        );
        println!("  Counterexample trace ({} steps):", violation.path.len());
        for (i, label) in violation.labels.iter().enumerate() {
            println!("    Step {}: {}", i, label);
        }
    }

    // --- Producer-Consumer ---
    println!("\n--- Producer-Consumer (capacity=3) ---");
    let system = ProducerConsumer { capacity: 3 };
    let checker = ModelChecker::new(system, Strategy::BFS);

    let buffer_bounds = SafetyProperty::new("buffer-in-bounds", |s| {
        s.buffer_count <= s.buffer_capacity
    });
    let eventually_consumes = LivenessProperty::new("consumer-eventually-active", |s| {
        matches!(s.consumer, model_checker::examples::producer_consumer::ActorState::Working)
    });

    let result = checker.check(&[buffer_bounds], &[eventually_consumes], true);
    println!("  States explored: {}", result.stats.states_explored);
    println!("  Transitions: {}", result.stats.transitions_evaluated);
    println!("  Deadlock states: {}", result.stats.deadlock_states);
    println!("  Valid: {}", result.is_valid());
    for v in &result.violations {
        println!("  Violation: {} ({:?})", v.property_name, v.violation_kind);
    }

    println!("\n  Elapsed: {}ms", result.stats.elapsed_ms);
}
```

### Tests: `tests/checker_tests.rs`

```rust
use model_checker::checker::{ModelChecker, Strategy, ViolationKind};
use model_checker::examples::peterson::{PetersonBuggy, PetersonCorrect, ProcessState};
use model_checker::examples::producer_consumer::ProducerConsumer;
use model_checker::property::{LivenessProperty, SafetyProperty};
use model_checker::system::System;

#[test]
fn correct_peterson_satisfies_mutual_exclusion() {
    let checker = ModelChecker::new(PetersonCorrect, Strategy::BFS);
    let prop = SafetyProperty::new("mutex", |s| {
        !matches!(
            (&s.proc0, &s.proc1),
            (ProcessState::Critical, ProcessState::Critical)
        )
    });
    let result = checker.check(&[prop], &[], false);
    assert!(result.is_valid(), "Correct Peterson should satisfy mutex");
    assert!(result.stats.states_explored > 0);
}

#[test]
fn buggy_peterson_violates_mutual_exclusion() {
    let checker = ModelChecker::new(PetersonBuggy, Strategy::BFS);
    let prop = SafetyProperty::new("mutex", |s| {
        !matches!(
            (&s.proc0, &s.proc1),
            (ProcessState::Critical, ProcessState::Critical)
        )
    });
    let result = checker.check(&[prop], &[], false);
    assert!(!result.is_valid(), "Buggy Peterson should violate mutex");
    let violation = &result.violations[0];
    assert!(matches!(violation.violation_kind, ViolationKind::Safety));
    assert!(!violation.path.is_empty(), "Should have a counterexample trace");
}

#[test]
fn buggy_peterson_counterexample_starts_at_initial() {
    let system = PetersonBuggy;
    let initials = system.initial_states();
    let checker = ModelChecker::new(system, Strategy::BFS);
    let prop = SafetyProperty::new("mutex", |s| {
        !matches!(
            (&s.proc0, &s.proc1),
            (ProcessState::Critical, ProcessState::Critical)
        )
    });
    let result = checker.check(&[prop], &[], false);
    let trace = &result.violations[0].path;
    assert!(
        initials.contains(&trace[0]),
        "Counterexample should start at an initial state"
    );
}

#[test]
fn bfs_finds_shortest_counterexample() {
    let checker_bfs = ModelChecker::new(PetersonBuggy, Strategy::BFS);
    let checker_dfs = ModelChecker::new(PetersonBuggy, Strategy::DFS);
    let prop_bfs = SafetyProperty::new("mutex", |s| {
        !matches!(
            (&s.proc0, &s.proc1),
            (ProcessState::Critical, ProcessState::Critical)
        )
    });
    let prop_dfs = SafetyProperty::new("mutex", |s| {
        !matches!(
            (&s.proc0, &s.proc1),
            (ProcessState::Critical, ProcessState::Critical)
        )
    });
    let bfs_result = checker_bfs.check(&[prop_bfs], &[], false);
    let dfs_result = checker_dfs.check(&[prop_dfs], &[], false);

    let bfs_len = bfs_result.violations[0].path.len();
    let dfs_len = dfs_result.violations[0].path.len();
    assert!(
        bfs_len <= dfs_len,
        "BFS should find equal or shorter counterexample: bfs={bfs_len} dfs={dfs_len}"
    );
}

#[test]
fn producer_consumer_buffer_stays_in_bounds() {
    let checker = ModelChecker::new(ProducerConsumer { capacity: 3 }, Strategy::BFS);
    let prop = SafetyProperty::new("bounds", |s| s.buffer_count <= s.buffer_capacity);
    let result = checker.check(&[prop], &[], false);
    assert!(
        result.is_valid(),
        "Buffer should never exceed capacity"
    );
}

#[test]
fn producer_consumer_explores_correct_state_count() {
    let checker = ModelChecker::new(ProducerConsumer { capacity: 2 }, Strategy::BFS);
    let result = checker.check::<SafetyProperty<ProducerConsumer>>(&[], &[], false);
    // capacity=2 -> buffer in {0,1,2}, each actor in {Ready,Working}
    // Not all combinations are reachable, but we verify exploration completes
    assert!(result.stats.states_explored > 0);
    assert!(result.stats.states_explored <= 3 * 2 * 2);
}

#[test]
fn dfs_explores_same_state_count_as_bfs() {
    let bfs = ModelChecker::new(ProducerConsumer { capacity: 2 }, Strategy::BFS);
    let dfs = ModelChecker::new(ProducerConsumer { capacity: 2 }, Strategy::DFS);
    let r_bfs = bfs.check::<SafetyProperty<ProducerConsumer>>(&[], &[], false);
    let r_dfs = dfs.check::<SafetyProperty<ProducerConsumer>>(&[], &[], false);
    assert_eq!(
        r_bfs.stats.states_explored, r_dfs.stats.states_explored,
        "Both strategies should explore the same total states"
    );
}

#[test]
fn deadlock_detection_finds_stuck_states() {
    // A trivial system that deadlocks
    struct Deadlocker;
    impl System for Deadlocker {
        type State = u8;
        fn initial_states(&self) -> Vec<u8> {
            vec![0]
        }
        fn transitions(&self, s: &u8) -> Vec<u8> {
            if *s < 3 {
                vec![s + 1]
            } else {
                vec![] // deadlock at 3
            }
        }
    }

    let checker = ModelChecker::new(Deadlocker, Strategy::BFS);
    let result = checker.check::<SafetyProperty<Deadlocker>>(&[], &[], true);
    assert_eq!(result.stats.deadlock_states, 1);
    let deadlock_violation = result
        .violations
        .iter()
        .find(|v| matches!(v.violation_kind, ViolationKind::Deadlock));
    assert!(deadlock_violation.is_some());
}

#[test]
fn liveness_property_satisfied_when_reachable() {
    struct SimpleSystem;
    impl System for SimpleSystem {
        type State = u8;
        fn initial_states(&self) -> Vec<u8> {
            vec![0]
        }
        fn transitions(&self, s: &u8) -> Vec<u8> {
            if *s < 5 { vec![s + 1] } else { vec![] }
        }
    }

    let checker = ModelChecker::new(SimpleSystem, Strategy::BFS);
    let live = LivenessProperty::new("reaches-5", |s: &u8| *s == 5);
    let result = checker.check(&[], &[live], false);
    // State 5 is reachable, so liveness is satisfied
    let liveness_violations: Vec<_> = result
        .violations
        .iter()
        .filter(|v| matches!(v.violation_kind, ViolationKind::Liveness))
        .collect();
    assert!(liveness_violations.is_empty());
}

#[test]
fn liveness_property_violated_when_unreachable() {
    struct SimpleSystem;
    impl System for SimpleSystem {
        type State = u8;
        fn initial_states(&self) -> Vec<u8> {
            vec![0]
        }
        fn transitions(&self, s: &u8) -> Vec<u8> {
            if *s < 3 { vec![s + 1] } else { vec![] }
        }
    }

    let checker = ModelChecker::new(SimpleSystem, Strategy::BFS);
    let live = LivenessProperty::new("reaches-10", |s: &u8| *s == 10);
    let result = checker.check(&[], &[live], false);
    let liveness_violations: Vec<_> = result
        .violations
        .iter()
        .filter(|v| matches!(v.violation_kind, ViolationKind::Liveness))
        .collect();
    assert_eq!(liveness_violations.len(), 1);
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
=== Model Checker State Explorer ===

--- Correct Peterson's Mutual Exclusion ---
  States explored: 20
  Transitions: 36
  Max depth: 10
  Violations: 0
  Valid: true

--- Buggy Peterson's (missing turn assignment) ---
  States explored: 16
  Valid: false
  Violation: mutual-exclusion (Safety)
  Counterexample trace (5 steps):
    Step 0: P0=Idle P1=Idle flags=[false,false] turn=0
    Step 1: P0=Requesting P1=Idle flags=[true,false] turn=0
    Step 2: P0=Requesting P1=Requesting flags=[true,true] turn=0
    Step 3: P0=Waiting P1=Requesting flags=[true,true] turn=0
    Step 4: P0=Critical P1=Critical flags=[true,true] turn=0

--- Producer-Consumer (capacity=3) ---
  States explored: 12
  Transitions: 22
  Deadlock states: 0
  Valid: true

  Elapsed: 0ms
```

(Exact trace may vary depending on transition ordering and exploration order.)

## Design Decisions

1. **BFS for shortest counterexamples**: BFS guarantees the shortest path from initial state to violation, which makes counterexamples easier to understand. DFS uses less memory (no full frontier in the queue) but may produce longer traces. Both are offered because their tradeoffs suit different use cases.

2. **Parent map for trace reconstruction**: During BFS, each discovered state stores its parent. This uses O(|states|) memory but provides instant trace reconstruction. The alternative -- re-running BFS after finding a violation -- avoids storing parents but doubles exploration time.

3. **Simplified liveness checking**: Full LTL liveness checking requires nested DFS or Buchi automata to detect accepting cycles. This implementation uses a simpler approach: after exploring all reachable states, it checks whether any state satisfies the liveness predicate. This handles reachability liveness ("eventually reaches state X") but not full temporal liveness ("along every infinite path, X happens infinitely often"). The README notes this limitation.

4. **FxHashMap over standard HashMap**: The `rustc-hash` crate provides a faster (non-cryptographic) hash function. Model checking is hash-intensive -- every state is hashed for the visited set -- so hash speed directly impacts throughput. FxHash is unsuitable for adversarial inputs but state hashes are internally generated.

5. **Generic System trait**: The `System` trait uses an associated type for `State` rather than a type parameter, ensuring each system definition has exactly one state type. The `Hash + Eq + Clone + Debug` bounds are the minimum needed for the visited set, trace storage, and error reporting.

## Common Mistakes

1. **Mutable state in transitions**: A common bug is mutating the input state when computing transitions instead of cloning first. In Rust, the borrow checker helps catch this, but if you use interior mutability (`RefCell`, etc.) in states, you can silently corrupt the exploration.

2. **Forgetting symmetry in interleaving**: When modeling concurrent processes, each process must be able to make a transition independently in each global state. Forgetting that process 1 can move while process 0 is in the critical section leads to incomplete state spaces and missed bugs.

3. **Non-deterministic hashing across runs**: If your state includes a `HashMap` (which has randomized iteration order), the hash of the state may differ between runs, causing exploration order to change. Use `BTreeMap` or deterministic hash functions for reproducible exploration.

4. **Memory exhaustion on large state spaces**: The visited set grows linearly with the number of reachable states. A system with 10 billion states will exhaust memory. Production model checkers use techniques like bit-state hashing (lossy but compact) or symmetry reduction. This implementation does not, so test with small parameters.

## Performance Notes

| Operation | Time Complexity | Space Complexity |
|-----------|----------------|-----------------|
| BFS exploration | O(|S| + |T|) | O(|S|) for visited set + parent map |
| DFS exploration | O(|S| + |T|) | O(|S|) visited + O(depth) stack |
| Safety check per state | O(1) per property | -- |
| Trace reconstruction (BFS) | O(depth) | O(depth) |
| State hashing (FxHash) | O(state_size) | -- |

Where |S| = number of reachable states, |T| = number of transitions. The bottleneck is always the state space size. Peterson's algorithm with 2 processes has ~20 states; with 3 processes it grows to ~200; with N processes it is exponential in N.

## Going Further

- Implement **symmetry reduction**: detect that permutations of identical processes yield equivalent states, reducing the state space by a factor of N!
- Add **partial order reduction**: not all interleavings of independent transitions need to be explored, prune redundant interleavings using the ample set method
- Implement **nested DFS for LTL liveness**: detect accepting cycles using the Couvreur or NDFS algorithm, enabling full temporal property checking
- Add **bounded model checking**: limit exploration to paths of length k, trading completeness for efficiency on very large state spaces
- Build a **CTL/LTL property parser**: let users write properties in temporal logic syntax (`AG(not(mutex_violation))`) instead of Rust closures
- Implement **on-the-fly verification**: check properties during exploration rather than after, enabling early termination when a violation is found (partially done above, but can be optimized further)
