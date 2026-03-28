# 9. Concurrent State Machine with Validation

<!--
difficulty: intermediate-advanced
category: concurrency-fundamentals
languages: [go]
concepts: [state-machines, goroutines, mutex, generics, event-driven-architecture]
estimated_time: 3-4 hours
bloom_level: analyze
prerequisites: [go-basics, goroutines, sync-package, generics, interfaces]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Go generics (type parameters, constraints)
- `sync.RWMutex` for concurrent read/write access
- Interface-based polymorphism in Go
- Observer pattern fundamentals

## Learning Objectives

- **Design** a type-safe finite state machine that leverages Go generics for compile-time state validation
- **Implement** thread-safe state transitions that serialize concurrent mutation attempts
- **Analyze** race conditions that arise when multiple goroutines attempt simultaneous transitions
- **Apply** entry/exit actions and transition guards to model complex business workflows
- **Create** a hierarchical state structure supporting substates with inherited transitions

## The Challenge

State machines appear everywhere in production systems: order processing workflows, connection lifecycle management, CI/CD pipeline stages, authentication flows. Most ad-hoc implementations use string comparisons and switch statements that silently accept invalid transitions. When multiple goroutines drive the same state machine, the problems multiply: race conditions corrupt state, transitions interleave, and guard conditions evaluate against stale data.

Your task is to build a typed finite state machine library in Go that makes invalid transitions impossible at the type level and unsafe concurrent access impossible at the runtime level. Each state defines entry and exit actions that execute atomically during transitions. Transitions have guard conditions that must be true for the transition to proceed. The machine emits typed events on every transition so external observers can react.

The machine must also support hierarchical states (substates). A "Processing" state might have substates "Validating", "Transforming", and "Persisting". Transitions defined on the parent state apply to all substates unless overridden. This is how you model complex workflows without a combinatorial explosion of transitions.

## Requirements

1. Define states and transitions as typed values (not strings) using Go generics or `iota` constants
2. Register valid transitions with optional guard functions: `func(currentState S, event E) bool`
3. Each state supports entry and exit action callbacks: `func(state S)`
4. Transition attempts from invalid states or with failing guards return a typed error (not nil/panic)
5. All state reads and transition attempts are safe for concurrent use by multiple goroutines
6. The machine emits transition events (from, to, event, timestamp) to registered observers
7. Implement hierarchical states: a parent state with substates, where parent transitions apply to all children
8. Provide a `Visualize()` method that returns a DOT-format graph of all states and transitions
9. Support `Reset()` to return to the initial state, executing exit actions for the current state and entry actions for the initial state
10. The machine must reject transitions while entry/exit actions are executing (no re-entrant transitions)

## Hints

<details>
<summary>Hint 1: Core machine structure with generics</summary>

```go
type StateMachine[S comparable, E comparable] struct {
    mu          sync.RWMutex
    current     S
    transitions map[S]map[E]Transition[S, E]
    entries     map[S]func(S)
    exits       map[S]func(S)
    observers   []func(TransitionEvent[S, E])
    transitioning bool
}
```

The double map `transitions[fromState][event]` gives O(1) lookup for valid transitions.
</details>

<details>
<summary>Hint 2: Atomic transition execution</summary>

Hold the write lock for the entire transition sequence: check guard, execute exit action, update state, execute entry action. This prevents observers from seeing intermediate states:

```go
func (sm *StateMachine[S, E]) Fire(event E) error {
    sm.mu.Lock()
    defer sm.mu.Unlock()

    if sm.transitioning {
        return ErrReentrantTransition
    }
    sm.transitioning = true
    defer func() { sm.transitioning = false }()

    // validate, exit, update, enter, notify
}
```
</details>

<details>
<summary>Hint 3: Hierarchical states via parent map</summary>

Store a `parent map[S]S` mapping. When looking up a transition for state X with event E, first check X directly, then walk up the parent chain:

```go
func (sm *StateMachine[S, E]) findTransition(state S, event E) (Transition[S, E], bool) {
    for s := state; ; {
        if trans, ok := sm.transitions[s][event]; ok {
            return trans, true
        }
        parent, hasParent := sm.parents[s]
        if !hasParent {
            return Transition[S, E]{}, false
        }
        s = parent
    }
}
```
</details>

<details>
<summary>Hint 4: DOT format visualization</summary>

```go
func (sm *StateMachine[S, E]) Visualize() string {
    var b strings.Builder
    b.WriteString("digraph FSM {\n")
    for from, events := range sm.transitions {
        for event, trans := range events {
            fmt.Fprintf(&b, "  %v -> %v [label=\"%v\"];\n", from, trans.To, event)
        }
    }
    b.WriteString("}\n")
    return b.String()
}
```
</details>

## Acceptance Criteria

- [ ] States and events are typed (not raw strings), invalid transitions return typed errors
- [ ] Entry/exit actions execute atomically during transitions
- [ ] Guard functions can prevent transitions, returning a descriptive error
- [ ] 100 concurrent goroutines firing transitions produce no races (`-race` flag passes)
- [ ] Hierarchical states inherit parent transitions correctly
- [ ] Observers receive transition events in order with correct from/to/event data
- [ ] `Visualize()` produces valid DOT graph output
- [ ] Re-entrant transitions (firing from within an action callback) are rejected
- [ ] `Reset()` executes proper exit/entry lifecycle

## Research Resources

- [Statecharts: A Visual Formalism for Complex Systems (David Harel, 1987)](https://www.sciencedirect.com/science/article/pii/0167642387900359) -- the foundational paper on hierarchical state machines
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share) -- concurrency philosophy for state protection
- [XState Documentation](https://xstate.js.org/docs/) -- modern statechart library (JavaScript, but excellent conceptual reference)
- [Graphviz DOT Language](https://graphviz.org/doc/info/lang.html) -- syntax reference for the visualization output
- [looplab/fsm](https://github.com/looplab/fsm) -- popular Go FSM library to study (your implementation should improve on its type safety)
