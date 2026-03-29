<!-- difficulty: advanced -->
<!-- category: formal-verification -->
<!-- languages: [rust] -->
<!-- concepts: [model-checking, state-machine, temporal-logic, graph-traversal, counterexample] -->
<!-- estimated_time: 10-14 hours -->
<!-- bloom_level: analyze, evaluate, create -->
<!-- prerequisites: [rust-enums, hash-maps, graph-algorithms, trait-objects, bfs-dfs] -->

# Challenge 117: Model Checker State Explorer

## Languages

Rust (stable, latest edition)

## Prerequisites

- Strong command of Rust enums, pattern matching, and trait objects
- Understanding of graph traversal algorithms (BFS, DFS)
- Familiarity with hash-based data structures (`HashMap`, `HashSet`)
- Experience with generic programming and trait bounds
- Basic knowledge of state machines and transition systems

## Learning Objectives

- **Analyze** concurrent systems by modeling them as finite state machines with explicit transitions
- **Implement** exhaustive state space exploration using BFS and DFS strategies
- **Evaluate** safety properties (invariants) and liveness properties (progress guarantees) against explored states
- **Create** a counterexample trace generator that produces human-readable violation paths
- **Design** efficient state hashing and deduplication for large state spaces

## The Challenge

Build an explicit-state model checker that verifies properties of finite state machines. Model checking is a formal verification technique: instead of testing a few inputs, you explore *every reachable state* of a system and check that desired properties hold universally.

Define systems as state machines with typed states and transition functions. Explore all reachable states via BFS or DFS, maintaining a visited set with state hashing. Check two categories of properties: safety properties (invariants that must hold in every reachable state) and liveness properties (something good must eventually happen along every execution path). Detect deadlock states where no transitions are enabled. When a property is violated, generate a counterexample trace from the initial state to the violating state.

The challenge of model checking is the state explosion problem -- even simple systems can have billions of reachable states. Your implementation must be efficient with hashing and avoid re-exploring visited states.

## Requirements

1. Define a `System` trait with associated types for `State` (must be `Hash + Eq + Clone + Debug`) and methods: `initial_states()`, `transitions(state) -> Vec<State>`, and `state_label(state) -> String`
2. Implement a `ModelChecker` that takes a `System` and explores all reachable states
3. Support both BFS (finds shortest counterexample) and DFS (uses less memory) exploration strategies
4. Maintain a visited set using state hashing to avoid re-exploration
5. Check safety properties: given `Fn(&State) -> bool`, verify the invariant holds in every reachable state
6. Check liveness properties: given `Fn(&State) -> bool`, verify that every execution path from the initial state eventually reaches a state satisfying the predicate
7. Detect deadlock: identify states with zero enabled transitions
8. Generate counterexample traces: on property violation, produce the full path from an initial state to the violating state
9. Report exploration statistics: total states explored, transitions evaluated, maximum search depth, time elapsed
10. Implement at least two example systems: a mutual exclusion protocol (e.g., Peterson's algorithm) and a producer-consumer bounded buffer
11. Demonstrate finding a bug: model a broken mutual exclusion protocol and show the checker finding the violation

## Hints

Safety and liveness are the two fundamental categories of temporal properties. A safety property says "nothing bad ever happens" -- formalized as an invariant that must hold in every reachable state. A liveness property says "something good eventually happens" -- every infinite execution must eventually reach a satisfying state. Deadlock detection is a special case of liveness violation.

For counterexample traces with BFS, store the parent pointer for each discovered state. When a violation is found, walk backward from the violating state to an initial state. With DFS, the current stack *is* the trace.

For liveness checking on finite state spaces, consider cycle detection. If you find a cycle where no state on the cycle satisfies the liveness predicate, that cycle is a counterexample (an infinite execution that never makes progress). Tarjan's algorithm or nested DFS can find such cycles.

The producer-consumer example is a good test: model a buffer of capacity N with producers and consumers as independent processes. States are tuples of (buffer_count, producer_state, consumer_state). The safety property is that the buffer count is always between 0 and N. The liveness property is that a consumer eventually consumes.

## Acceptance Criteria

- [ ] `System` trait allows defining state machines with typed states and transitions
- [ ] BFS exploration finds the shortest counterexample path
- [ ] DFS exploration correctly explores the full state space
- [ ] Visited set prevents re-exploration of states (verified by checking state count)
- [ ] Safety property violations produce a counterexample trace from initial state to violation
- [ ] Liveness property checking detects paths/cycles that never reach the target predicate
- [ ] Deadlock detection identifies states with no outgoing transitions
- [ ] Counterexample traces are human-readable with state labels
- [ ] Exploration statistics are reported (states, transitions, depth, time)
- [ ] Mutual exclusion example demonstrates finding a safety violation in a buggy protocol
- [ ] Producer-consumer example verifies correct buffer bounds
- [ ] All tests pass with `cargo test`

## Research Resources

- [Model Checking (Clarke, Grumberg, Peled)](https://mitpress.mit.edu/9780262038836/model-checking/) -- the definitive textbook on model checking theory
- [The SPIN Model Checker (Holzmann)](https://spinroot.com/spin/whatispin.html) -- the most widely used explicit-state model checker
- [TLA+ Video Course (Lamport)](https://lamport.azurewebsites.net/video/videos.html) -- Leslie Lamport's course on specifying and verifying systems
- [Peterson's Algorithm (Wikipedia)](https://en.wikipedia.org/wiki/Peterson%27s_algorithm) -- classic mutual exclusion protocol, good modeling target
- [Nested Depth-First Search for LTL model checking](https://link.springer.com/chapter/10.1007/3-540-61042-1_37) -- algorithm for detecting accepting cycles
- [State Explosion Problem](https://en.wikipedia.org/wiki/Model_checking#Techniques) -- overview of techniques to combat state space blowup
- [Rust `rustc_hash` crate](https://docs.rs/rustc-hash/latest/rustc_hash/) -- fast hashing for hash sets used in state deduplication
