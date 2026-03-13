# 18. Property-Based Testing

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-17 (traits, generics, error handling, serde, lifetimes)
- Comfortable writing standard unit tests with `#[test]` and assertions
- Familiarity with `Cargo.toml` dependency management
- Understanding of generics and trait bounds

## Learning Objectives

- Understand the philosophy of property-based testing vs example-based testing
- Use the `proptest` crate to generate arbitrary test inputs
- Write property invariants that must hold for all generated values
- Leverage shrinking to find minimal failing inputs automatically
- Test serialization roundtrips (serialize then deserialize = identity)
- Test data structure invariants under random mutations
- Compose custom strategies for domain-specific types
- Evaluate when property-based testing adds value vs adds noise

## Concepts

### The Problem with Example-Based Tests

Traditional unit tests verify specific input/output pairs:

```rust
#[test]
fn test_reverse() {
    assert_eq!(reverse("hello"), "olleh");
    assert_eq!(reverse(""), "");
    assert_eq!(reverse("a"), "a");
}
```

This tests three cases. But `reverse` accepts any `&str`. You are betting that your three chosen examples cover every important behavior. Property-based testing flips the approach: instead of choosing inputs, you describe *properties* that must hold for *all* inputs, and the framework generates hundreds or thousands of random inputs to verify them.

### Core Idea: Properties, Not Examples

A property is a universal statement about your code:

- "Reversing a string twice returns the original" -- `reverse(reverse(s)) == s`
- "Sorting a list makes it non-decreasing" -- `sorted[i] <= sorted[i+1]` for all i
- "Serializing then deserializing returns the original value" -- `deserialize(serialize(x)) == x`

If the framework finds an input that violates the property, it *shrinks* the input to find the smallest example that still fails. This minimal counterexample is far more useful for debugging than a random 500-character string.

### proptest Basics

```rust
use proptest::prelude::*;

proptest! {
    #[test]
    fn reverse_twice_is_identity(s in "\\PC*") {
        let reversed: String = s.chars().rev().collect();
        let double_reversed: String = reversed.chars().rev().collect();
        prop_assert_eq!(s, double_reversed);
    }
}
```

The `"\\PC*"` is a regex strategy: it generates arbitrary strings of printable characters. `proptest!` runs the test body with many generated values (256 by default). If `prop_assert_eq!` fails, proptest shrinks `s` to the minimal counterexample.

### Strategies: Controlling Input Generation

Strategies define how to generate values. proptest provides built-in strategies for all primitive types and common containers:

```rust
use proptest::prelude::*;
use proptest::collection::vec;

proptest! {
    // Generate integers in a range
    #[test]
    fn positive_values(x in 1..=1000i32) {
        prop_assert!(x > 0);
        prop_assert!(x <= 1000);
    }

    // Generate vectors with constrained length
    #[test]
    fn vec_length(v in vec(any::<i32>(), 0..50)) {
        prop_assert!(v.len() < 50);
    }

    // Generate tuples
    #[test]
    fn tuple_values((a, b) in (1..100i32, 1..100i32)) {
        prop_assert!(a + b >= 2);
        prop_assert!(a + b <= 198);
    }

    // Generate optional values
    #[test]
    fn optional_string(opt in proptest::option::of("\\w+")) {
        if let Some(ref s) = opt {
            prop_assert!(!s.is_empty());
        }
    }
}
```

### Custom Strategies with `prop_compose!`

For domain types, compose strategies from primitives:

```rust
use proptest::prelude::*;

#[derive(Debug, Clone, PartialEq)]
struct Money {
    cents: u64,
    currency: String,
}

prop_compose! {
    fn money_strategy()(
        cents in 0..1_000_000u64,
        currency in prop_oneof![
            Just("USD".to_string()),
            Just("EUR".to_string()),
            Just("GBP".to_string()),
        ]
    ) -> Money {
        Money { cents, currency }
    }
}

proptest! {
    #[test]
    fn money_is_non_negative(m in money_strategy()) {
        // cents is u64, so always >= 0, but this verifies our strategy
        prop_assert!(m.cents < 1_000_000);
        prop_assert!(["USD", "EUR", "GBP"].contains(&m.currency.as_str()));
    }
}
```

### Implementing Arbitrary for Your Types

For types you control, implement `Arbitrary` to make them directly usable with `any::<YourType>()`:

```rust
use proptest::prelude::*;

#[derive(Debug, Clone, PartialEq)]
struct UserId(u64);

#[derive(Debug, Clone, PartialEq)]
struct Email {
    local: String,
    domain: String,
}

impl Email {
    fn full(&self) -> String {
        format!("{}@{}", self.local, self.domain)
    }
}

impl Arbitrary for Email {
    type Parameters = ();
    type Strategy = BoxedStrategy<Self>;

    fn arbitrary_with(_: Self::Parameters) -> Self::Strategy {
        (
            "[a-z][a-z0-9]{1,10}",           // local part
            "[a-z]{2,8}\\.(com|org|net)",     // domain
        )
            .prop_map(|(local, domain)| Email { local, domain })
            .boxed()
    }
}

proptest! {
    #[test]
    fn email_always_contains_at(email in any::<Email>()) {
        prop_assert!(email.full().contains('@'));
        prop_assert!(!email.local.is_empty());
        prop_assert!(email.domain.contains('.'));
    }
}
```

### Shrinking: Finding Minimal Counterexamples

Shrinking is the killer feature. When a test fails, proptest does not just hand you the random input -- it systematically reduces the input to the smallest value that still triggers the failure:

```rust
use proptest::prelude::*;

fn buggy_sum(values: &[i32]) -> i32 {
    // Bug: overflow on large sums
    values.iter().copied().sum()
}

proptest! {
    #[test]
    fn sum_matches_manual(v in proptest::collection::vec(-100..100i32, 0..100)) {
        let result = buggy_sum(&v);
        let expected: i64 = v.iter().map(|&x| x as i64).sum();
        prop_assert_eq!(result as i64, expected);
    }
}
```

Without shrinking, the failure might report a vector of 87 elements. With shrinking, proptest narrows it down to something like `[100, 100, ..., 100]` with the minimum length that triggers overflow. This makes the bug immediately obvious.

### Roundtrip Testing: The Canonical Use Case

The most common property in real codebases: serialization roundtrips.

```rust
use proptest::prelude::*;
use serde::{Serialize, Deserialize};

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
struct Config {
    name: String,
    port: u16,
    debug: bool,
    tags: Vec<String>,
}

impl Arbitrary for Config {
    type Parameters = ();
    type Strategy = BoxedStrategy<Self>;

    fn arbitrary_with(_: Self::Parameters) -> Self::Strategy {
        (
            "[a-zA-Z][a-zA-Z0-9_]{0,20}",
            1024..65535u16,
            any::<bool>(),
            proptest::collection::vec("[a-z]{1,10}", 0..5),
        )
            .prop_map(|(name, port, debug, tags)| Config { name, port, debug, tags })
            .boxed()
    }
}

proptest! {
    #[test]
    fn json_roundtrip(config in any::<Config>()) {
        let json = serde_json::to_string(&config).unwrap();
        let decoded: Config = serde_json::from_str(&json).unwrap();
        prop_assert_eq!(config, decoded);
    }

    #[test]
    fn bincode_roundtrip(config in any::<Config>()) {
        let bytes = bincode::serialize(&config).unwrap();
        let decoded: Config = bincode::deserialize(&bytes).unwrap();
        prop_assert_eq!(config, decoded);
    }
}
```

This finds bugs that example tests miss: special characters in strings that break JSON escaping, edge-case port values, empty vectors, unicode in names.

### Testing Data Structure Invariants

Property tests excel at verifying structural invariants -- conditions that must hold after any sequence of operations:

```rust
use proptest::prelude::*;

#[derive(Debug, Clone)]
struct SortedVec {
    inner: Vec<i32>,
}

impl SortedVec {
    fn new() -> Self {
        SortedVec { inner: Vec::new() }
    }

    fn insert(&mut self, value: i32) {
        match self.inner.binary_search(&value) {
            Ok(pos) | Err(pos) => self.inner.insert(pos, value),
        }
    }

    fn contains(&self, value: &i32) -> bool {
        self.inner.binary_search(value).is_ok()
    }

    fn is_sorted(&self) -> bool {
        self.inner.windows(2).all(|w| w[0] <= w[1])
    }

    fn len(&self) -> usize {
        self.inner.len()
    }
}

proptest! {
    #[test]
    fn sorted_vec_stays_sorted(
        operations in proptest::collection::vec(-1000..1000i32, 0..200)
    ) {
        let mut sv = SortedVec::new();
        for op in &operations {
            sv.insert(*op);
            // Invariant: sorted after every insertion
            prop_assert!(sv.is_sorted(),
                "not sorted after inserting {}: {:?}", op, sv.inner);
        }
        // Invariant: all inserted values are findable
        for op in &operations {
            prop_assert!(sv.contains(op),
                "missing value {} in {:?}", op, sv.inner);
        }
    }

    #[test]
    fn sorted_vec_length_matches_unique_count(
        values in proptest::collection::vec(-100..100i32, 0..50)
    ) {
        let mut sv = SortedVec::new();
        for v in &values {
            sv.insert(*v);
        }
        // Our insert allows duplicates, so length == total insertions
        prop_assert_eq!(sv.len(), values.len());
    }
}
```

### Filtering and Preconditions

Sometimes a property only holds for a subset of inputs. Use `prop_assume!` to skip inputs that do not meet preconditions:

```rust
use proptest::prelude::*;

fn divide(a: i64, b: i64) -> i64 {
    a / b
}

proptest! {
    #[test]
    fn division_undone_by_multiplication(a in any::<i64>(), b in any::<i64>()) {
        // Skip division by zero
        prop_assume!(b != 0);
        // Skip overflow cases
        prop_assume!(a != i64::MIN || b != -1);

        let result = divide(a, b);
        // Integer division truncates, so we check the weaker property:
        // result * b is within b of a
        let diff = (a - result * b).abs();
        prop_assert!(diff < b.abs(),
            "a={a}, b={b}, result={result}, diff={diff}");
    }
}
```

Avoid filtering more than ~20% of inputs. If most inputs are discarded, write a more targeted strategy instead.

### Configuring Test Runs

Control the number of test cases and other parameters:

```rust
use proptest::prelude::*;

// Per-test configuration
proptest! {
    // Override the default number of cases (256)
    #![proptest_config(ProptestConfig::with_cases(1000))]

    #[test]
    fn stress_test(v in proptest::collection::vec(any::<u8>(), 0..1000)) {
        // Runs 1000 times instead of 256
        prop_assert!(v.len() < 1000);
    }
}
```

You can also set `PROPTEST_CASES=5000` as an environment variable to override globally in CI.

### Regression Files

When proptest finds a failing case, it writes it to a `proptest-regressions/` file so that specific case is always replayed on future runs:

```text
# Seed file for proptest regression
# This file was generated by proptest.
cc f4b5a2e1d3c6...
```

Commit these files to version control. They ensure that once a bug is found, its specific counterexample is tested forever.

### Comparison: Property-Based vs Example-Based

| Axis | Example-Based | Property-Based |
|------|---------------|----------------|
| Coverage | Manually chosen inputs | Hundreds of random inputs per run |
| Bug discovery | Finds bugs you anticipated | Finds bugs you did not anticipate |
| Readability | Clear input/output pairs | Requires understanding properties |
| Debugging | Failure input is known | Shrinking finds minimal input |
| Speed | Fast (few cases) | Slower (many cases) |
| Flakiness | Deterministic | Deterministic (seed-based) |
| Maintenance | Update when behavior changes | Properties often survive refactors |
| Best for | Known edge cases, business rules | Invariants, roundtrips, parsers |

### When Property-Based Testing Shines

1. **Parsers and serializers**: roundtrip property catches encoding bugs
2. **Data structures**: invariants (sorted, balanced, unique) must hold after any operation
3. **Numeric code**: overflow, precision, edge cases at boundaries
4. **Protocol implementations**: encode/decode, compression, encryption
5. **Refactoring**: properties stay valid even when internals change

### When Property-Based Testing is Not Worth It

1. **Simple CRUD**: the property is just "it does what it does" -- tautological
2. **UI behavior**: hard to express visual correctness as a property
3. **Integration tests**: external systems do not tolerate random inputs
4. **When you cannot state the property**: if you cannot articulate what must always be true, an example test is more honest

## Exercises

### Exercise 1: Serialization Roundtrip for a Domain Model

Build a domain model for a task management system and verify serialization roundtrips across two formats.

**Cargo.toml:**
```toml
[package]
name = "proptest-exercises"
version = "0.1.0"
edition = "2021"

[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
bincode = "1"
proptest = "1"
```

**Types:**
- `Priority` enum: `Low`, `Medium`, `High`, `Critical`
- `Status` enum: `Todo`, `InProgress`, `Done`, `Cancelled`
- `Task` struct: `id: u64`, `title: String`, `priority: Priority`, `status: Status`, `tags: Vec<String>`, `estimate_hours: Option<f32>`

**Requirements:**
1. Implement `Arbitrary` for all three types
2. Constrain `title` to `[a-zA-Z0-9 ]{1,50}` (no special chars that could break formats)
3. Constrain `tags` to 0-5 elements, each `[a-z]{1,10}`
4. Constrain `estimate_hours` to `Some(0.5..=100.0)` or `None`
5. Write a JSON roundtrip property test
6. Write a bincode roundtrip property test
7. Write a property: "serialized JSON always contains the task id"

**Hints:**
- `prop_oneof!` works well for enum strategies
- For `Option<f32>`, use `proptest::option::of(0.5f32..=100.0)`
- `prop_assert!` for boolean checks, `prop_assert_eq!` for equality

<details>
<summary>Solution</summary>

```rust
use serde::{Serialize, Deserialize};
use proptest::prelude::*;

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
enum Priority {
    Low,
    Medium,
    High,
    Critical,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
enum Status {
    Todo,
    InProgress,
    Done,
    Cancelled,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
struct Task {
    id: u64,
    title: String,
    priority: Priority,
    status: Status,
    tags: Vec<String>,
    estimate_hours: Option<f32>,
}

impl Arbitrary for Priority {
    type Parameters = ();
    type Strategy = BoxedStrategy<Self>;

    fn arbitrary_with(_: Self::Parameters) -> Self::Strategy {
        prop_oneof![
            Just(Priority::Low),
            Just(Priority::Medium),
            Just(Priority::High),
            Just(Priority::Critical),
        ]
        .boxed()
    }
}

impl Arbitrary for Status {
    type Parameters = ();
    type Strategy = BoxedStrategy<Self>;

    fn arbitrary_with(_: Self::Parameters) -> Self::Strategy {
        prop_oneof![
            Just(Status::Todo),
            Just(Status::InProgress),
            Just(Status::Done),
            Just(Status::Cancelled),
        ]
        .boxed()
    }
}

impl Arbitrary for Task {
    type Parameters = ();
    type Strategy = BoxedStrategy<Self>;

    fn arbitrary_with(_: Self::Parameters) -> Self::Strategy {
        (
            any::<u64>(),                                    // id
            "[a-zA-Z0-9 ]{1,50}",                           // title
            any::<Priority>(),                               // priority
            any::<Status>(),                                 // status
            proptest::collection::vec("[a-z]{1,10}", 0..5),  // tags
            proptest::option::of(0.5f32..=100.0),            // estimate_hours
        )
            .prop_map(|(id, title, priority, status, tags, estimate_hours)| {
                Task { id, title, priority, status, tags, estimate_hours }
            })
            .boxed()
    }
}

proptest! {
    #[test]
    fn json_roundtrip(task in any::<Task>()) {
        let json = serde_json::to_string(&task).unwrap();
        let decoded: Task = serde_json::from_str(&json).unwrap();
        prop_assert_eq!(&task, &decoded);
    }

    #[test]
    fn bincode_roundtrip(task in any::<Task>()) {
        let bytes = bincode::serialize(&task).unwrap();
        let decoded: Task = bincode::deserialize(&bytes).unwrap();
        prop_assert_eq!(&task, &decoded);
    }

    #[test]
    fn json_contains_task_id(task in any::<Task>()) {
        let json = serde_json::to_string(&task).unwrap();
        let id_str = task.id.to_string();
        prop_assert!(json.contains(&id_str),
            "JSON should contain task id {}: {}", task.id, json);
    }

    #[test]
    fn task_title_is_never_empty(task in any::<Task>()) {
        prop_assert!(!task.title.is_empty(),
            "title should not be empty: {:?}", task);
    }

    #[test]
    fn tags_within_bounds(task in any::<Task>()) {
        prop_assert!(task.tags.len() < 5,
            "too many tags: {}", task.tags.len());
        for tag in &task.tags {
            prop_assert!(tag.len() <= 10,
                "tag too long: {}", tag);
        }
    }
}

fn main() {
    println!("Run `cargo test` to execute property-based tests.");
}

#[cfg(test)]
mod tests {
    use super::*;

    // Traditional example-based test for comparison
    #[test]
    fn manual_roundtrip() {
        let task = Task {
            id: 42,
            title: "Fix bug".into(),
            priority: Priority::High,
            status: Status::InProgress,
            tags: vec!["urgent".into(), "backend".into()],
            estimate_hours: Some(3.5),
        };
        let json = serde_json::to_string(&task).unwrap();
        let decoded: Task = serde_json::from_str(&json).unwrap();
        assert_eq!(task, decoded);
    }
}
```

**Why this works:** The `Arbitrary` impls constrain generation to valid domain values. The roundtrip property ensures that serde's derive macros handle every combination correctly -- not just the two or three you would test manually. If you added a new variant to `Priority` and forgot to update the `Arbitrary` impl, the tests would not cover it, but adding it to `prop_oneof!` is a one-line change.
</details>

### Exercise 2: Data Structure Invariants

Build a `BoundedQueue<T>` -- a fixed-capacity queue that rejects pushes when full. Verify its structural invariants with property-based testing.

**Requirements:**
1. `BoundedQueue::new(capacity: usize)` creates a queue with fixed max size
2. `push(&mut self, value: T) -> Result<(), T>` returns `Err(value)` when full
3. `pop(&mut self) -> Option<T>` returns `None` when empty
4. `len(&self) -> usize` and `is_empty(&self) -> bool`
5. FIFO ordering: push 1,2,3 then pop returns 1,2,3

**Properties to verify:**
- Length never exceeds capacity
- After push, `is_empty()` is false (unless capacity is 0)
- After successful push, length increases by 1
- After successful pop, length decreases by 1
- Push-then-pop-all returns values in FIFO order
- Pushing to a full queue returns `Err` and does not change length

**Hints:**
- Generate a sequence of `enum Op { Push(i32), Pop }` to test arbitrary operation sequences
- Use `prop_compose!` for the operation strategy

<details>
<summary>Solution</summary>

```rust
use std::collections::VecDeque;
use proptest::prelude::*;

#[derive(Debug)]
struct BoundedQueue<T> {
    inner: VecDeque<T>,
    capacity: usize,
}

impl<T> BoundedQueue<T> {
    fn new(capacity: usize) -> Self {
        BoundedQueue {
            inner: VecDeque::with_capacity(capacity),
            capacity,
        }
    }

    fn push(&mut self, value: T) -> Result<(), T> {
        if self.inner.len() >= self.capacity {
            return Err(value);
        }
        self.inner.push_back(value);
        Ok(())
    }

    fn pop(&mut self) -> Option<T> {
        self.inner.pop_front()
    }

    fn len(&self) -> usize {
        self.inner.len()
    }

    fn is_empty(&self) -> bool {
        self.inner.is_empty()
    }

    fn capacity(&self) -> usize {
        self.capacity
    }
}

#[derive(Debug, Clone)]
enum Op {
    Push(i32),
    Pop,
}

fn op_strategy() -> impl Strategy<Value = Op> {
    prop_oneof![
        any::<i32>().prop_map(Op::Push),
        Just(Op::Pop),
    ]
}

proptest! {
    #![proptest_config(ProptestConfig::with_cases(500))]

    #[test]
    fn length_never_exceeds_capacity(
        capacity in 1..50usize,
        ops in proptest::collection::vec(op_strategy(), 0..200)
    ) {
        let mut q = BoundedQueue::new(capacity);
        for op in ops {
            match op {
                Op::Push(v) => { let _ = q.push(v); }
                Op::Pop => { let _ = q.pop(); }
            }
            prop_assert!(q.len() <= q.capacity(),
                "len {} > capacity {}", q.len(), q.capacity());
        }
    }

    #[test]
    fn push_to_full_queue_returns_err_and_preserves_length(
        capacity in 1..20usize,
        values in proptest::collection::vec(any::<i32>(), 1..50)
    ) {
        let mut q = BoundedQueue::new(capacity);
        for v in values {
            let len_before = q.len();
            match q.push(v) {
                Ok(()) => {
                    prop_assert_eq!(q.len(), len_before + 1);
                }
                Err(returned) => {
                    prop_assert_eq!(returned, v, "returned value should be the rejected value");
                    prop_assert_eq!(q.len(), len_before, "length should not change on rejection");
                    prop_assert_eq!(q.len(), capacity, "should only reject when full");
                }
            }
        }
    }

    #[test]
    fn push_makes_non_empty(
        capacity in 1..20usize,
        value in any::<i32>()
    ) {
        let mut q = BoundedQueue::new(capacity);
        q.push(value).unwrap();
        prop_assert!(!q.is_empty());
    }

    #[test]
    fn fifo_ordering(
        capacity in 1..50usize,
        values in proptest::collection::vec(any::<i32>(), 0..50)
    ) {
        let mut q = BoundedQueue::new(capacity);
        let mut pushed = Vec::new();

        for v in &values {
            if q.push(*v).is_ok() {
                pushed.push(*v);
            }
        }

        let mut popped = Vec::new();
        while let Some(v) = q.pop() {
            popped.push(v);
        }

        prop_assert_eq!(&pushed, &popped,
            "FIFO order violated: pushed {:?}, popped {:?}", pushed, popped);
    }

    #[test]
    fn pop_from_empty_returns_none(capacity in 0..20usize) {
        let mut q: BoundedQueue<i32> = BoundedQueue::new(capacity);
        prop_assert!(q.pop().is_none());
        prop_assert!(q.is_empty());
    }

    #[test]
    fn pop_decreases_length(
        capacity in 1..20usize,
        count in 1..20usize
    ) {
        let count = count.min(capacity);
        let mut q = BoundedQueue::new(capacity);
        for i in 0..count {
            q.push(i as i32).unwrap();
        }
        for _ in 0..count {
            let len_before = q.len();
            let val = q.pop();
            prop_assert!(val.is_some());
            prop_assert_eq!(q.len(), len_before - 1);
        }
    }

    #[test]
    fn zero_capacity_rejects_everything(value in any::<i32>()) {
        let mut q = BoundedQueue::new(0);
        prop_assert!(q.push(value).is_err());
        prop_assert!(q.is_empty());
        prop_assert_eq!(q.len(), 0);
    }

    #[test]
    fn interleaved_push_pop_maintains_invariants(
        capacity in 1..30usize,
        ops in proptest::collection::vec(op_strategy(), 0..300)
    ) {
        let mut q = BoundedQueue::new(capacity);
        let mut model: VecDeque<i32> = VecDeque::new(); // reference model

        for op in ops {
            match op {
                Op::Push(v) => {
                    if model.len() < capacity {
                        model.push_back(v);
                        prop_assert!(q.push(v).is_ok());
                    } else {
                        prop_assert!(q.push(v).is_err());
                    }
                }
                Op::Pop => {
                    let expected = model.pop_front();
                    let actual = q.pop();
                    prop_assert_eq!(actual, expected);
                }
            }
            prop_assert_eq!(q.len(), model.len());
        }
    }
}

fn main() {
    println!("Run `cargo test` to execute property-based tests.");
}
```

**Key insight: model-based testing.** The `interleaved_push_pop_maintains_invariants` test uses a `VecDeque` as a reference model. Every operation is applied to both the implementation and the model, then their states are compared. This is the most powerful pattern in property-based testing -- your model is simple and obviously correct, and proptest verifies the real implementation matches it under random operations.
</details>

### Exercise 3: Testing a Parser with Shrinking

Build a simple expression parser and evaluator, then use property-based testing to find edge cases.

**Grammar:**
- Expressions are `i64` literals, addition (`+`), multiplication (`*`), and parentheses
- Examples: `42`, `1+2`, `3*4+5`, `(1+2)*3`

**Requirements:**
1. Write a `parse(input: &str) -> Result<Expr, String>` function
2. Write an `eval(expr: &Expr) -> i64` function
3. Generate random `Expr` trees and format them to strings
4. Property: `eval(parse(format(expr))) == eval(expr)` (parse-format roundtrip preserves semantics)
5. Property: `eval` of a single literal `n` returns `n`
6. Property: addition is commutative -- `eval(a + b) == eval(b + a)`
7. Observe shrinking: introduce a deliberate bug and see what minimal case proptest finds

**Hints:**
- Use a recursive strategy with `prop_recursive` for `Expr` generation
- Limit recursion depth to avoid stack overflow
- `prop_recursive(depth, desired_size, expected_branch_size, leaf_strategy)`

<details>
<summary>Solution</summary>

```rust
use proptest::prelude::*;

#[derive(Debug, Clone, PartialEq)]
enum Expr {
    Lit(i64),
    Add(Box<Expr>, Box<Expr>),
    Mul(Box<Expr>, Box<Expr>),
}

impl Expr {
    fn format(&self) -> String {
        match self {
            Expr::Lit(n) => n.to_string(),
            Expr::Add(a, b) => format!("({}+{})", a.format(), b.format()),
            Expr::Mul(a, b) => format!("({}*{})", a.format(), b.format()),
        }
    }
}

fn eval(expr: &Expr) -> i64 {
    match expr {
        Expr::Lit(n) => *n,
        Expr::Add(a, b) => eval(a).wrapping_add(eval(b)),
        Expr::Mul(a, b) => eval(a).wrapping_mul(eval(b)),
    }
}

// --- Simple recursive descent parser ---
// Grammar: expr = term (('+') term)*
//          term = atom (('*') atom)*
//          atom = number | '(' expr ')'

struct Parser<'a> {
    input: &'a [u8],
    pos: usize,
}

impl<'a> Parser<'a> {
    fn new(input: &'a str) -> Self {
        Parser { input: input.as_bytes(), pos: 0 }
    }

    fn peek(&self) -> Option<u8> {
        self.input.get(self.pos).copied()
    }

    fn advance(&mut self) {
        self.pos += 1;
    }

    fn skip_whitespace(&mut self) {
        while self.pos < self.input.len() && self.input[self.pos] == b' ' {
            self.pos += 1;
        }
    }

    fn parse_expr(&mut self) -> Result<Expr, String> {
        let mut left = self.parse_term()?;
        self.skip_whitespace();
        while self.peek() == Some(b'+') {
            self.advance();
            let right = self.parse_term()?;
            left = Expr::Add(Box::new(left), Box::new(right));
            self.skip_whitespace();
        }
        Ok(left)
    }

    fn parse_term(&mut self) -> Result<Expr, String> {
        let mut left = self.parse_atom()?;
        self.skip_whitespace();
        while self.peek() == Some(b'*') {
            self.advance();
            let right = self.parse_atom()?;
            left = Expr::Mul(Box::new(left), Box::new(right));
            self.skip_whitespace();
        }
        Ok(left)
    }

    fn parse_atom(&mut self) -> Result<Expr, String> {
        self.skip_whitespace();
        match self.peek() {
            Some(b'(') => {
                self.advance();
                let expr = self.parse_expr()?;
                self.skip_whitespace();
                if self.peek() != Some(b')') {
                    return Err(format!("expected ')' at pos {}", self.pos));
                }
                self.advance();
                Ok(expr)
            }
            Some(b'-') | Some(b'0'..=b'9') => {
                let start = self.pos;
                if self.peek() == Some(b'-') {
                    self.advance();
                }
                while matches!(self.peek(), Some(b'0'..=b'9')) {
                    self.advance();
                }
                let s = std::str::from_utf8(&self.input[start..self.pos])
                    .map_err(|e| e.to_string())?;
                let n: i64 = s.parse().map_err(|e: std::num::ParseIntError| e.to_string())?;
                Ok(Expr::Lit(n))
            }
            Some(c) => Err(format!("unexpected char '{}' at pos {}", c as char, self.pos)),
            None => Err("unexpected end of input".to_string()),
        }
    }
}

fn parse(input: &str) -> Result<Expr, String> {
    let mut parser = Parser::new(input);
    let expr = parser.parse_expr()?;
    parser.skip_whitespace();
    if parser.pos != parser.input.len() {
        return Err(format!("trailing input at pos {}", parser.pos));
    }
    Ok(expr)
}

// --- Strategy for generating Expr trees ---

fn expr_strategy() -> impl Strategy<Value = Expr> {
    // Leaf: small literals to avoid overflow in interesting ways
    let leaf = (-100..100i64).prop_map(Expr::Lit);

    leaf.prop_recursive(
        4,   // max depth
        64,  // desired size
        2,   // expected branch size
        |inner| {
            prop_oneof![
                // 40% chance of addition
                (inner.clone(), inner.clone())
                    .prop_map(|(a, b)| Expr::Add(Box::new(a), Box::new(b))),
                // 40% chance of multiplication
                (inner.clone(), inner)
                    .prop_map(|(a, b)| Expr::Mul(Box::new(a), Box::new(b))),
            ]
        },
    )
}

proptest! {
    #[test]
    fn parse_format_roundtrip_preserves_eval(expr in expr_strategy()) {
        let formatted = expr.format();
        let parsed = parse(&formatted)
            .map_err(|e| TestCaseError::Fail(format!("parse failed: {e} for input: {formatted}").into()))?;
        let original_val = eval(&expr);
        let parsed_val = eval(&parsed);
        prop_assert_eq!(original_val, parsed_val,
            "eval mismatch for '{formatted}': original={original_val}, parsed={parsed_val}");
    }

    #[test]
    fn literal_evaluates_to_itself(n in -10000..10000i64) {
        let expr = Expr::Lit(n);
        prop_assert_eq!(eval(&expr), n);
    }

    #[test]
    fn addition_is_commutative(a in expr_strategy(), b in expr_strategy()) {
        let ab = eval(&Expr::Add(Box::new(a.clone()), Box::new(b.clone())));
        let ba = eval(&Expr::Add(Box::new(b), Box::new(a)));
        prop_assert_eq!(ab, ba);
    }

    #[test]
    fn multiplication_is_commutative(a in expr_strategy(), b in expr_strategy()) {
        let ab = eval(&Expr::Mul(Box::new(a.clone()), Box::new(b.clone())));
        let ba = eval(&Expr::Mul(Box::new(b), Box::new(a)));
        prop_assert_eq!(ab, ba);
    }

    #[test]
    fn addition_identity(a in expr_strategy()) {
        let result = eval(&Expr::Add(Box::new(a.clone()), Box::new(Expr::Lit(0))));
        prop_assert_eq!(result, eval(&a));
    }

    #[test]
    fn multiplication_identity(a in expr_strategy()) {
        let result = eval(&Expr::Mul(Box::new(a.clone()), Box::new(Expr::Lit(1))));
        prop_assert_eq!(result, eval(&a));
    }

    #[test]
    fn multiplication_by_zero(a in expr_strategy()) {
        let result = eval(&Expr::Mul(Box::new(a), Box::new(Expr::Lit(0))));
        prop_assert_eq!(result, 0);
    }
}

fn main() {
    // Demonstrate formatting and parsing
    let expr = Expr::Add(
        Box::new(Expr::Lit(1)),
        Box::new(Expr::Mul(
            Box::new(Expr::Lit(2)),
            Box::new(Expr::Lit(3)),
        )),
    );
    let formatted = expr.format();
    println!("Expression: {formatted}");
    println!("Evaluated:  {}", eval(&expr));

    let parsed = parse(&formatted).unwrap();
    println!("Parsed:     {}", eval(&parsed));
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_simple_literal() {
        let expr = parse("42").unwrap();
        assert_eq!(eval(&expr), 42);
    }

    #[test]
    fn parse_negative_literal() {
        let expr = parse("-7").unwrap();
        assert_eq!(eval(&expr), -7);
    }

    #[test]
    fn parse_addition() {
        let expr = parse("1+2").unwrap();
        assert_eq!(eval(&expr), 3);
    }

    #[test]
    fn parse_precedence() {
        // Multiplication binds tighter than addition
        let expr = parse("1+2*3").unwrap();
        assert_eq!(eval(&expr), 7);
    }

    #[test]
    fn parse_parentheses() {
        let expr = parse("(1+2)*3").unwrap();
        assert_eq!(eval(&expr), 9);
    }

    #[test]
    fn parse_error_on_trailing() {
        assert!(parse("42 abc").is_err());
    }
}
```

**Shrinking in action:** If you introduced a bug (e.g., making `Mul` evaluate as `a * b + 1`), proptest would find a failing case and shrink it. Instead of reporting `((3+(-72))*(((5)*(-1))+42))`, it would shrink to `(0*0)` -- the simplest expression where the bug manifests. The shrunk output tells you immediately: "multiplication of zero times zero should be zero, but got 1."
</details>

## Trade-Off Analysis

### proptest vs quickcheck

| Axis | proptest | quickcheck |
|------|----------|------------|
| Strategy composition | `prop_compose!`, combinators | `Arbitrary` trait |
| Shrinking | Integrated, always works | Requires manual `Shrink` impl |
| Regex strategies | Built-in (`"[a-z]+"`) | Not available |
| Recursive types | `prop_recursive` | Manual with `sized` |
| Configuration | `ProptestConfig`, env vars | `quickcheck!` macro, `TestResult` |
| Ecosystem adoption | Dominant in 2025 | Older, less maintained |
| Compile time | Heavier (regex + strategies) | Lighter |

**Recommendation:** Use proptest unless you have a specific reason not to. Its shrinking and strategy composition are superior.

### Cost-Benefit of Property-Based Testing

| Cost | Benefit |
|------|---------|
| Slower test suite (hundreds of iterations) | Finds bugs you did not think of |
| Strategy code adds maintenance burden | Properties survive refactors better than examples |
| Learning curve for strategy composition | Shrinking gives you minimal reproduction cases |
| False sense of security if properties are weak | Forces you to articulate invariants precisely |

### Combining Both Approaches

The best test suites use both:

```rust
// Example test: specific known edge case, documents expected behavior
#[test]
fn empty_input_returns_none() {
    assert_eq!(parse(""), Err("unexpected end of input".into()));
}

// Property test: verifies invariant across the input space
proptest! {
    #[test]
    fn parse_format_roundtrip(expr in expr_strategy()) {
        let formatted = expr.format();
        let parsed = parse(&formatted).unwrap();
        prop_assert_eq!(eval(&expr), eval(&parsed));
    }
}
```

Example tests document behavior for human readers. Property tests cover the space that human imagination misses.

## Common Mistakes

1. **Writing tautological properties.** `prop_assert_eq!(f(x), f(x))` tests nothing. Your property must relate the output to something independent -- a model, an inverse operation, or a structural invariant.

2. **Generating unconstrained inputs for constrained domains.** If your function only accepts ASCII strings, generating arbitrary UTF-8 (the default) wastes most test iterations on `prop_assume!` rejections. Write a targeted strategy instead.

3. **Ignoring shrinking regressions.** When proptest finds a failure, it writes a regression file. Commit these files. They ensure the minimal counterexample runs on every future `cargo test`.

4. **Testing too many properties in one test.** Each `proptest!` block should test one property. Multiple `prop_assert!` calls checking different invariants make failures harder to diagnose.

5. **Unbounded recursive strategies.** Without depth limits, recursive strategies can generate structures that overflow the stack. Always pass a finite depth to `prop_recursive`.

## Verification

```bash
# Run all property-based tests
cargo test

# Run with more iterations (overrides default 256)
PROPTEST_CASES=2000 cargo test

# Run a specific test
cargo test json_roundtrip

# Run in release mode (property tests are CPU-bound)
cargo test --release

# Check for clippy warnings
cargo clippy -- -D warnings
```

## What You Learned

- **Property-based testing** generates random inputs to verify universal properties, complementing example-based tests
- **proptest** provides strategies, shrinking, and regression tracking out of the box
- **Roundtrip testing** (serialize/deserialize, parse/format) is the most common and highest-value property pattern
- **Model-based testing** compares your implementation against a simple reference model under random operations
- **Shrinking** automatically reduces failing inputs to minimal counterexamples, making bugs immediately obvious
- **Strategy composition** with `prop_compose!`, `prop_oneof!`, and `Arbitrary` lets you generate domain-specific values
- **Cost vs benefit**: property tests are slower but find bugs you did not anticipate; combine them with example tests for full coverage

## What's Next

Exercise 19 covers memory layout optimization -- understanding how Rust lays out structs and enums in memory, controlling alignment, and designing data structures for cache performance.

## Resources

- [proptest book](https://proptest-rs.github.io/proptest/intro.html)
- [proptest crate docs](https://docs.rs/proptest)
- [Property-Based Testing in Rust (blog)](https://fsharpforfunandprofit.com/pbt/) -- concepts from F# that apply to Rust
- [The Design and Use of Strategies (proptest)](https://proptest-rs.github.io/proptest/proptest/strategy.html)
- [quickcheck crate](https://docs.rs/quickcheck) -- alternative PBT library
- [Hypothesis (Python)](https://hypothesis.readthedocs.io/) -- inspiration for proptest's design
