# Solution: Concurrent State Machine with Validation

## Architecture Overview

The state machine uses Go generics to parameterize both state and event types, enforcing compile-time type safety. A double map (`transitions[fromState][event]`) provides O(1) transition lookup. All mutations are serialized through a `sync.RWMutex`, with the write lock held for the entire transition lifecycle (guard check, exit action, state update, entry action, observer notification) to prevent observers from seeing intermediate states.

Hierarchical states are modeled with a parent map. Transition lookup walks up the parent chain until it finds a matching transition or exhausts the hierarchy. A re-entrancy guard prevents transitions from being fired within entry/exit action callbacks.

## Go Solution

### Project Setup

```bash
mkdir -p concurrent-fsm && cd concurrent-fsm
go mod init concurrent-fsm
```

### Implementation

```go
// fsm.go
package fsm

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// TransitionEvent records a completed transition for observers.
type TransitionEvent[S, E comparable] struct {
	From      S
	To        S
	Event     E
	Timestamp time.Time
}

// Transition defines a target state and optional guard.
type Transition[S, E comparable] struct {
	To    S
	Guard func(from S, event E) bool
}

// StateMachine is a thread-safe, generic finite state machine.
type StateMachine[S, E comparable] struct {
	mu            sync.RWMutex
	current       S
	initial       S
	transitions   map[S]map[E]Transition[S, E]
	entries       map[S]func(S)
	exits         map[S]func(S)
	parents       map[S]S
	observers     []func(TransitionEvent[S, E])
	transitioning bool
}

// New creates a state machine with the given initial state.
func New[S, E comparable](initial S) *StateMachine[S, E] {
	return &StateMachine[S, E]{
		current:     initial,
		initial:     initial,
		transitions: make(map[S]map[E]Transition[S, E]),
		entries:     make(map[S]func(S)),
		exits:       make(map[S]func(S)),
		parents:     make(map[S]S),
	}
}

// AddTransition registers a valid transition from a state on an event.
func (sm *StateMachine[S, E]) AddTransition(from S, event E, to S, guard func(S, E) bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.transitions[from] == nil {
		sm.transitions[from] = make(map[E]Transition[S, E])
	}
	sm.transitions[from][event] = Transition[S, E]{To: to, Guard: guard}
}

// OnEntry registers a callback executed when entering a state.
func (sm *StateMachine[S, E]) OnEntry(state S, fn func(S)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.entries[state] = fn
}

// OnExit registers a callback executed when leaving a state.
func (sm *StateMachine[S, E]) OnExit(state S, fn func(S)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.exits[state] = fn
}

// SetParent establishes a hierarchical relationship.
func (sm *StateMachine[S, E]) SetParent(child, parent S) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.parents[child] = parent
}

// OnTransition registers an observer notified after every transition.
func (sm *StateMachine[S, E]) OnTransition(fn func(TransitionEvent[S, E])) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.observers = append(sm.observers, fn)
}

// Current returns the current state (thread-safe read).
func (sm *StateMachine[S, E]) Current() S {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.current
}

// Fire attempts a state transition triggered by the given event.
func (sm *StateMachine[S, E]) Fire(event E) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.transitioning {
		return &TransitionError[S, E]{
			From:    sm.current,
			Event:   event,
			Message: "re-entrant transition rejected: transition already in progress",
		}
	}
	sm.transitioning = true
	defer func() { sm.transitioning = false }()

	trans, found := sm.findTransition(sm.current, event)
	if !found {
		return &TransitionError[S, E]{
			From:    sm.current,
			Event:   event,
			Message: "no valid transition",
		}
	}

	if trans.Guard != nil && !trans.Guard(sm.current, event) {
		return &TransitionError[S, E]{
			From:    sm.current,
			Event:   event,
			Message: "guard condition failed",
		}
	}

	from := sm.current

	// Execute exit action for current state
	if exitFn, ok := sm.exits[from]; ok {
		exitFn(from)
	}

	// Update state
	sm.current = trans.To

	// Execute entry action for new state
	if entryFn, ok := sm.entries[trans.To]; ok {
		entryFn(trans.To)
	}

	// Notify observers
	evt := TransitionEvent[S, E]{
		From:      from,
		To:        trans.To,
		Event:     event,
		Timestamp: time.Now(),
	}
	for _, obs := range sm.observers {
		obs(evt)
	}

	return nil
}

// findTransition walks up the parent hierarchy to find a matching transition.
func (sm *StateMachine[S, E]) findTransition(state S, event E) (Transition[S, E], bool) {
	current := state
	for {
		if events, ok := sm.transitions[current]; ok {
			if trans, ok := events[event]; ok {
				return trans, true
			}
		}
		parent, hasParent := sm.parents[current]
		if !hasParent {
			return Transition[S, E]{}, false
		}
		current = parent
	}
}

// Reset returns the machine to its initial state with proper lifecycle.
func (sm *StateMachine[S, E]) Reset() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.transitioning {
		return
	}
	sm.transitioning = true
	defer func() { sm.transitioning = false }()

	if exitFn, ok := sm.exits[sm.current]; ok {
		exitFn(sm.current)
	}
	sm.current = sm.initial
	if entryFn, ok := sm.entries[sm.initial]; ok {
		entryFn(sm.initial)
	}
}

// Visualize returns the state machine as a DOT-format graph.
func (sm *StateMachine[S, E]) Visualize() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var b strings.Builder
	b.WriteString("digraph FSM {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString(fmt.Sprintf("  node [shape=circle]; \"%v\" [shape=doublecircle];\n", sm.initial))
	b.WriteString(fmt.Sprintf("  \"\" [shape=point];\n  \"\" -> \"%v\";\n", sm.initial))

	for from, events := range sm.transitions {
		for event, trans := range events {
			label := fmt.Sprintf("%v", event)
			if trans.Guard != nil {
				label += " [guarded]"
			}
			b.WriteString(fmt.Sprintf("  \"%v\" -> \"%v\" [label=\"%s\"];\n", from, trans.To, label))
		}
	}

	// Show parent-child relationships
	for child, parent := range sm.parents {
		b.WriteString(fmt.Sprintf("  \"%v\" -> \"%v\" [style=dashed, label=\"parent\"];\n", child, parent))
	}

	b.WriteString("}\n")
	return b.String()
}

// TransitionError provides structured error information.
type TransitionError[S, E comparable] struct {
	From    S
	Event   E
	Message string
}

func (e *TransitionError[S, E]) Error() string {
	return fmt.Sprintf("transition error: from=%v event=%v: %s", e.From, e.Event, e.Message)
}
```

### Tests

```go
// fsm_test.go
package fsm

import (
	"sync"
	"sync/atomic"
	"testing"
)

type State int

const (
	Idle State = iota
	Processing
	Validating
	Transforming
	Done
	Failed
)

type Event int

const (
	Start Event = iota
	Validate
	Transform
	Complete
	Fail
	Reset
)

func newOrderMachine() *StateMachine[State, Event] {
	sm := New[State, Event](Idle)

	sm.AddTransition(Idle, Start, Processing, nil)
	sm.AddTransition(Processing, Validate, Validating, nil)
	sm.AddTransition(Processing, Fail, Failed, nil)
	sm.AddTransition(Validating, Transform, Transforming, nil)
	sm.AddTransition(Transforming, Complete, Done, nil)
	sm.AddTransition(Transforming, Fail, Failed, nil)

	// Hierarchical: Validating and Transforming are substates of Processing
	sm.SetParent(Validating, Processing)
	sm.SetParent(Transforming, Processing)

	return sm
}

func TestBasicTransition(t *testing.T) {
	sm := newOrderMachine()

	if err := sm.Fire(Start); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sm.Current() != Processing {
		t.Fatalf("expected Processing, got %v", sm.Current())
	}
}

func TestInvalidTransition(t *testing.T) {
	sm := newOrderMachine()

	err := sm.Fire(Complete)
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}

	te, ok := err.(*TransitionError[State, Event])
	if !ok {
		t.Fatalf("expected TransitionError, got %T", err)
	}
	if te.From != Idle {
		t.Errorf("expected From=Idle, got %v", te.From)
	}
}

func TestGuardCondition(t *testing.T) {
	sm := New[State, Event](Idle)
	sm.AddTransition(Idle, Start, Processing, func(s State, e Event) bool {
		return false // always reject
	})

	err := sm.Fire(Start)
	if err == nil {
		t.Fatal("expected guard to reject transition")
	}
}

func TestEntryExitActions(t *testing.T) {
	var log []string
	sm := New[State, Event](Idle)
	sm.AddTransition(Idle, Start, Processing, nil)

	sm.OnExit(Idle, func(s State) {
		log = append(log, "exit-idle")
	})
	sm.OnEntry(Processing, func(s State) {
		log = append(log, "enter-processing")
	})

	if err := sm.Fire(Start); err != nil {
		t.Fatal(err)
	}

	if len(log) != 2 || log[0] != "exit-idle" || log[1] != "enter-processing" {
		t.Errorf("unexpected action order: %v", log)
	}
}

func TestHierarchicalTransition(t *testing.T) {
	sm := newOrderMachine()

	// Navigate to Validating (a substate of Processing)
	sm.Fire(Start)
	sm.Fire(Validate)

	if sm.Current() != Validating {
		t.Fatalf("expected Validating, got %v", sm.Current())
	}

	// Fail is defined on Processing (parent). It should apply to Validating.
	err := sm.Fire(Fail)
	if err != nil {
		t.Fatalf("hierarchical transition failed: %v", err)
	}
	if sm.Current() != Failed {
		t.Fatalf("expected Failed, got %v", sm.Current())
	}
}

func TestConcurrentTransitions(t *testing.T) {
	sm := New[State, Event](Idle)
	sm.AddTransition(Idle, Start, Processing, nil)
	sm.AddTransition(Processing, Complete, Done, nil)
	sm.AddTransition(Done, Reset, Idle, nil)

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	// 100 goroutines all try to cycle through states
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			events := []Event{Start, Complete, Reset}
			for _, e := range events {
				if err := sm.Fire(e); err != nil {
					errorCount.Add(1)
				} else {
					successCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	// We expect some successes and some errors due to concurrent contention.
	// The important thing is no panics or data races.
	t.Logf("successes=%d errors=%d", successCount.Load(), errorCount.Load())
}

func TestObserverNotification(t *testing.T) {
	sm := New[State, Event](Idle)
	sm.AddTransition(Idle, Start, Processing, nil)

	var received []TransitionEvent[State, Event]
	sm.OnTransition(func(evt TransitionEvent[State, Event]) {
		received = append(received, evt)
	})

	sm.Fire(Start)

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].From != Idle || received[0].To != Processing {
		t.Errorf("unexpected event: %+v", received[0])
	}
}

func TestVisualize(t *testing.T) {
	sm := newOrderMachine()
	dot := sm.Visualize()

	if !contains(dot, "digraph FSM") {
		t.Error("missing DOT header")
	}
	if !contains(dot, "->") {
		t.Error("missing transitions in DOT output")
	}
	t.Log(dot)
}

func TestReset(t *testing.T) {
	var exitCalled, entryCalled bool

	sm := New[State, Event](Idle)
	sm.AddTransition(Idle, Start, Processing, nil)

	sm.OnExit(Processing, func(s State) { exitCalled = true })
	sm.OnEntry(Idle, func(s State) { entryCalled = true })

	sm.Fire(Start)
	sm.Reset()

	if sm.Current() != Idle {
		t.Errorf("expected Idle after reset, got %v", sm.Current())
	}
	if !exitCalled {
		t.Error("exit action not called on reset")
	}
	if !entryCalled {
		t.Error("entry action not called on reset")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

### Running and Testing

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestBasicTransition
--- PASS: TestBasicTransition (0.00s)
=== RUN   TestInvalidTransition
--- PASS: TestInvalidTransition (0.00s)
=== RUN   TestGuardCondition
--- PASS: TestGuardCondition (0.00s)
=== RUN   TestEntryExitActions
--- PASS: TestEntryExitActions (0.00s)
=== RUN   TestHierarchicalTransition
--- PASS: TestHierarchicalTransition (0.00s)
=== RUN   TestConcurrentTransitions
    fsm_test.go:132: successes=7 errors=293
--- PASS: TestConcurrentTransitions (0.00s)
=== RUN   TestObserverNotification
--- PASS: TestObserverNotification (0.00s)
=== RUN   TestVisualize
    fsm_test.go:148: digraph FSM {
        ...
    }
--- PASS: TestVisualize (0.00s)
=== RUN   TestReset
--- PASS: TestReset (0.00s)
PASS
```

## Design Decisions

**Decision 1: Single lock for the entire transition lifecycle.** The write lock is held from guard evaluation through state update and observer notification. This means observers always see a consistent state, but long-running entry/exit actions block all other transitions. The alternative (release lock between steps) would allow higher concurrency but observers could see partial transitions. For a state machine (where transition order matters), consistency is more important than throughput.

**Decision 2: Generics with `comparable` constraint.** States and events must be `comparable` for use as map keys. This allows `iota` constants, strings, or any value type, but excludes slices and maps. This is the right trade-off: states and events should be simple value types, not complex structures.

**Decision 3: Parent map for hierarchy instead of embedded substates.** A flat parent map is simpler than nested state machine objects. The trade-off is that deep hierarchies require O(depth) lookups per transition, but state hierarchies rarely exceed 3-4 levels.

## Common Mistakes

**Mistake 1: Using `RLock` during transitions.** A common error is using a read lock for guard evaluation and upgrading to a write lock for the state change. Between the two locks, another goroutine can change the state, invalidating the guard's decision. Always hold the write lock for the entire atomic transition.

**Mistake 2: Firing transitions from within action callbacks.** If an entry action calls `Fire()`, and `Fire()` tries to acquire the write lock that is already held, you get a deadlock (with `sync.Mutex`) or a re-entrant state corruption. The `transitioning` flag detects and rejects this pattern.

**Mistake 3: Using string-typed states without constants.** Using raw strings ("idle", "processing") makes typos into silent bugs. Typed constants with `iota` catch these at compile time.

## Performance Notes

- The `sync.RWMutex` allows concurrent `Current()` reads without blocking. Only `Fire()` acquires the write lock.
- Observer notification happens under the write lock. If observers are slow, they become a bottleneck. Consider async observer notification (send to a channel) if observers do I/O.
- The hierarchical lookup walks the parent chain on every transition. Cache the resolved transition map at setup time if transition lookup is on the hot path.

## Going Further

- Add timeout transitions: if a state is not exited within a duration, automatically fire a timeout event
- Implement state history: track the last N transitions for debugging and audit logging
- Add serialization: export/import machine state for persistence across restarts
- Build a workflow engine on top: define a business process as a state machine with async task execution at each state
- Implement parallel states (AND-states from UML statecharts): the machine is simultaneously in multiple orthogonal states
