# 18. Property-Based Testing

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-17 (traits, generics, error handling, type-level guarantees)
- Experience writing unit tests with `#[test]` and assertions
- Understanding of `Result`, `Option`, and iterator adapters
- Familiarity with serialization (`serde`) and basic data structures

## Learning Objectives

- Distinguish property-based testing from example-based testing and identify when each is appropriate
- Implement the five fundamental property categories: roundtrip, idempotency, invariant, commutativity, and oracle
- Write custom proptest strategies for domain types with constrained generation
- Understand shrinking and how it finds minimal failing cases
- Use `cargo-fuzz` for coverage-guided fuzzing of unsafe code and parsers
- Integrate `insta` snapshot testing for complex output verification
- Measure test effectiveness with `cargo-mutants` (mutation testing) and `cargo-llvm-cov` (code coverage)

## Concepts

### Why Property-Based Testing

Example-based tests verify specific inputs produce specific outputs. Property-based tests verify that **properties** hold for **all** inputs in a domain. A property is a universally quantified statement: "for all valid inputs X, f(X) satisfies P."

```rust
// Example-based: tests ONE case
#[test]
fn sort_specific() {
    assert_eq!(sort(vec![3, 1, 2]), vec![1, 2, 3]);
}

// Property-based: tests THOUSANDS of cases
// "for all Vec<i32>, sorting produces a vec where each element <= the next"
proptest! {
    #[test]
    fn sort_is_ordered(mut v in prop::collection::vec(any::<i32>(), 0..100)) {
        let sorted = sort(v);
        for w in sorted.windows(2) {
            assert!(w[0] <= w[1]);
        }
    }
}
```

The second test is strictly more powerful: it discovers edge cases you would never think to write by hand (empty vecs, single-element, duplicates, i32::MIN, i32::MAX).

### The Five Property Categories

#### 1. Roundtrip (Encode/Decode)

If you encode X then decode the result, you get X back:

```rust
// For all T: decode(encode(x)) == x
proptest! {
    #[test]
    fn json_roundtrip(s in "\\PC{0,100}") {
        let encoded = serde_json::to_string(&s).unwrap();
        let decoded: String = serde_json::from_str(&encoded).unwrap();
        prop_assert_eq!(s, decoded);
    }
}
```

#### 2. Idempotency

Applying an operation twice produces the same result as once:

```rust
// For all T: f(f(x)) == f(x)
proptest! {
    #[test]
    fn normalize_is_idempotent(s in "[a-zA-Z ]{0,50}") {
        let once = normalize(&s);
        let twice = normalize(&once);
        prop_assert_eq!(once, twice);
    }
}
```

#### 3. Invariant Preservation

An operation maintains a structural invariant:

```rust
// For all operations on a BTreeMap, keys remain sorted
proptest! {
    #[test]
    fn btree_always_sorted(
        ops in prop::collection::vec(
            (any::<i32>(), any::<i32>()),
            0..50
        )
    ) {
        let mut map = std::collections::BTreeMap::new();
        for (k, v) in ops {
            map.insert(k, v);
        }
        let keys: Vec<_> = map.keys().collect();
        for w in keys.windows(2) {
            prop_assert!(w[0] <= w[1]);
        }
    }
}
```

#### 4. Commutativity / Algebraic Laws

Operations satisfy mathematical properties:

```rust
// For all a, b: merge(a, b) == merge(b, a)
proptest! {
    #[test]
    fn merge_is_commutative(
        a in prop::collection::hash_set(any::<i32>(), 0..20),
        b in prop::collection::hash_set(any::<i32>(), 0..20),
    ) {
        let ab = merge(&a, &b);
        let ba = merge(&b, &a);
        prop_assert_eq!(ab, ba);
    }
}
```

#### 5. Oracle (Test Against Reference Implementation)

Compare your optimized implementation against a known-correct but slow one:

```rust
// For all inputs: fast_sort(x) == std_sort(x)
proptest! {
    #[test]
    fn custom_sort_matches_std(mut v in prop::collection::vec(any::<i32>(), 0..200)) {
        let mut expected = v.clone();
        expected.sort();
        custom_sort(&mut v);
        prop_assert_eq!(v, expected);
    }
}
```

### proptest: Strategies and Shrinking

proptest generates random values using **strategies**. A strategy is a recipe for producing values of a type. When a test fails, proptest **shrinks** the input -- finding the simplest value that still triggers the failure.

Built-in strategies:

```rust
use proptest::prelude::*;

// Primitives
any::<i32>()                              // all i32 values
0..100i32                                  // range
prop_oneof![Just(0), 1..100i32]           // weighted choice

// Strings
"[a-z]{3,10}"                             // regex-based
any::<String>()                            // arbitrary unicode
"\\PC{0,50}"                              // printable chars

// Collections
prop::collection::vec(any::<i32>(), 0..20)     // Vec with size range
prop::collection::hash_map(any::<String>(), any::<i32>(), 0..10)

// Options and Results
prop::option::of(any::<i32>())
```

Custom strategies for domain types:

```rust
use proptest::prelude::*;

#[derive(Debug, Clone, PartialEq)]
struct Money {
    cents: u64,
    currency: String,
}

fn money_strategy() -> impl Strategy<Value = Money> {
    let currency = prop_oneof![
        Just("USD".to_string()),
        Just("EUR".to_string()),
        Just("GBP".to_string()),
    ];
    (0..100_000_00u64, currency).prop_map(|(cents, currency)| Money { cents, currency })
}

proptest! {
    #[test]
    fn money_display_roundtrip(m in money_strategy()) {
        let s = format!("{}.{:02} {}", m.cents / 100, m.cents % 100, m.currency);
        prop_assert!(s.contains(&m.currency));
    }
}
```

### proptest `#[property_test]` Attribute (1.9+)

proptest 1.9 introduced a proc-macro attribute as an alternative to the `proptest!` block macro:

```rust
use proptest::prelude::*;
use proptest::property_test;

#[property_test]
fn addition_is_commutative(a: i32, b: i32) {
    // Parameters use their Arbitrary impl by default
    prop_assert_eq!(a.wrapping_add(b), b.wrapping_add(a));
}

#[property_test]
fn custom_strategy(#[strategy(1..100u32)] x: u32) {
    prop_assert!(x > 0);
    prop_assert!(x < 100);
}
```

### Shrinking in Practice

When proptest finds a failing input, it does not stop. It systematically reduces the input while the test still fails:

```
# Initial failure: vec![847293, -12938, 0, 44, -999999, 2]
# Shrinking...
# Step 1: vec![0, -12938, 0, 44, -999999, 2]
# Step 2: vec![0, -1, 0, 0, -999999, 0]
# Step 3: vec![0, -1, 0, 0, -1, 0]
# Step 4: vec![0, -1]          <-- minimal failing case
```

This is far more useful for debugging than a random 50-element vector. Shrinking works automatically for built-in types. For custom strategies, `prop_map` preserves shrinking of the underlying strategy.

### cargo-fuzz: Coverage-Guided Fuzzing

While proptest generates random inputs, `cargo-fuzz` uses LLVM's coverage instrumentation to guide input generation toward unexplored code paths. It is the right tool for parsers, deserializers, and any code with complex branching.

```bash
cargo install cargo-fuzz
cargo fuzz init
cargo fuzz add my_target
```

A fuzz target (`fuzz/fuzz_targets/my_target.rs`):

```rust
#![no_main]
use libfuzzer_sys::fuzz_target;

fuzz_target!(|data: &[u8]| {
    if let Ok(s) = std::str::from_utf8(data) {
        let _ = my_crate::parse_config(s);
    }
});
```

```bash
cargo +nightly fuzz run my_target -- -max_len=4096
```

Key differences from proptest:
- **cargo-fuzz** explores code coverage, not properties. It finds crashes and panics.
- **proptest** verifies semantic properties with shrinking. It finds logical bugs.
- Use both: cargo-fuzz for "does it crash?" and proptest for "is the output correct?"

### insta: Snapshot Testing

`insta` captures the output of an expression and compares it against a stored snapshot. When the output changes, you review and accept or reject the change:

```rust
use insta::assert_snapshot;
use insta::assert_json_snapshot;

#[test]
fn test_error_display() {
    let err = MyError::NotFound { id: 42 };
    assert_snapshot!(err.to_string());
}

#[test]
fn test_json_output() {
    let result = process_data(&input);
    assert_json_snapshot!(result);
}
```

Snapshots are stored in `tests/snapshots/` as `.snap` files. Workflow:

```bash
cargo test                     # Tests fail if snapshots differ
cargo insta review             # Interactive review of changes
cargo insta accept             # Accept all pending changes
```

insta is complementary to property testing: use properties for universal invariants, snapshots for complex outputs where "correct" is hard to express as a predicate but easy to review visually.

### cargo-mutants: Mutation Testing

Mutation testing answers: "if I inject a bug, does any test catch it?" `cargo-mutants` systematically modifies your source code (replacing `+` with `-`, `true` with `false`, deleting statements) and checks that at least one test fails for each mutation.

```bash
cargo install cargo-mutants
cargo mutants                  # Run all mutations
cargo mutants --file src/lib.rs  # Mutate specific file
```

Output:

```
Found 47 mutants
  38 caught (tests failed as expected)
   6 missed (tests passed -- bug not caught!)
   3 timeout
```

Missed mutants reveal undertested code paths. This is more valuable than code coverage because it proves your assertions are meaningful, not just that the code was executed.

### cargo-llvm-cov: Code Coverage

```bash
cargo install cargo-llvm-cov
cargo llvm-cov                           # Summary
cargo llvm-cov --html                    # HTML report
cargo llvm-cov --open                    # Open in browser
cargo llvm-cov --fail-under-lines 80     # CI gate
```

Coverage tells you which lines were executed during tests. High coverage with weak assertions is meaningless (cargo-mutants catches this). Low coverage reliably indicates untested code. Use both tools together.

## Exercises

### Exercise 1: Roundtrip and Idempotency Properties

Build a `Slug` type that converts strings to URL-safe slugs (lowercase, hyphens instead of spaces, no special chars). Test with roundtrip and idempotency properties.

```toml
[package]
name = "property-testing"
version = "0.1.0"
edition = "2024"

[dependencies]
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"

[dev-dependencies]
proptest = "1.6"
insta = { version = "1.42", features = ["json"] }
```

**Requirements:**
- `slugify(input: &str) -> String`: lowercases, replaces whitespace/punctuation with `-`, collapses multiple hyphens, trims leading/trailing hyphens
- Property 1 (idempotency): `slugify(slugify(x)) == slugify(x)` for all strings
- Property 2 (invariant): output contains only `[a-z0-9-]`
- Property 3 (no leading/trailing hyphens): output does not start or end with `-`
- Property 4 (roundtrip): for a serializable struct with a slug field, JSON encode/decode roundtrips
- Minimum 4 property tests + 3 example-based edge case tests
- Use `insta` snapshot test for 5 specific inputs

<details>
<summary>Solution</summary>

```rust
use serde::{Serialize, Deserialize};

pub fn slugify(input: &str) -> String {
    let mut result = String::with_capacity(input.len());
    let mut last_was_hyphen = true; // prevents leading hyphen

    for ch in input.chars() {
        if ch.is_ascii_alphanumeric() {
            result.push(ch.to_ascii_lowercase());
            last_was_hyphen = false;
        } else if !last_was_hyphen {
            result.push('-');
            last_was_hyphen = true;
        }
    }

    // Remove trailing hyphen
    if result.ends_with('-') {
        result.pop();
    }

    result
}

#[derive(Debug, Serialize, Deserialize, PartialEq)]
pub struct Article {
    pub title: String,
    pub slug: String,
}

impl Article {
    pub fn new(title: &str) -> Self {
        Article {
            slug: slugify(title),
            title: title.to_string(),
        }
    }
}

fn main() {
    let examples = [
        "Hello, World!",
        "  Multiple   Spaces  ",
        "CamelCaseTitle",
        "---already---slugged---",
        "Special!@#$%^&*()Characters",
        "",
    ];

    for input in &examples {
        println!("{:30} -> {:?}", format!("{input:?}"), slugify(input));
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use proptest::prelude::*;

    // --- Property-based tests ---

    proptest! {
        #[test]
        fn idempotency(s in "\\PC{0,100}") {
            let once = slugify(&s);
            let twice = slugify(&once);
            prop_assert_eq!(
                once, twice,
                "slugify is not idempotent for input: {:?}", s
            );
        }

        #[test]
        fn output_only_valid_chars(s in "\\PC{0,100}") {
            let slug = slugify(&s);
            for ch in slug.chars() {
                prop_assert!(
                    ch.is_ascii_lowercase() || ch.is_ascii_digit() || ch == '-',
                    "invalid char {:?} in slug {:?} from input {:?}", ch, slug, s
                );
            }
        }

        #[test]
        fn no_leading_trailing_hyphens(s in "\\PC{0,100}") {
            let slug = slugify(&s);
            if !slug.is_empty() {
                prop_assert!(
                    !slug.starts_with('-'),
                    "leading hyphen in slug {:?} from input {:?}", slug, s
                );
                prop_assert!(
                    !slug.ends_with('-'),
                    "trailing hyphen in slug {:?} from input {:?}", slug, s
                );
            }
        }

        #[test]
        fn no_consecutive_hyphens(s in "\\PC{0,100}") {
            let slug = slugify(&s);
            prop_assert!(
                !slug.contains("--"),
                "consecutive hyphens in slug {:?} from input {:?}", slug, s
            );
        }

        #[test]
        fn article_json_roundtrip(title in "[a-zA-Z0-9 ]{1,50}") {
            let article = Article::new(&title);
            let json = serde_json::to_string(&article).unwrap();
            let decoded: Article = serde_json::from_str(&json).unwrap();
            prop_assert_eq!(article, decoded);
        }

        #[test]
        fn slug_length_bounded(s in "\\PC{0,200}") {
            let slug = slugify(&s);
            // Slug can never be longer than input (we only shrink or replace chars)
            prop_assert!(
                slug.len() <= s.len(),
                "slug {:?} longer than input {:?}", slug, s
            );
        }
    }

    // --- Example-based edge cases ---

    #[test]
    fn empty_input() {
        assert_eq!(slugify(""), "");
    }

    #[test]
    fn only_special_chars() {
        assert_eq!(slugify("!@#$%^&*()"), "");
    }

    #[test]
    fn single_char() {
        assert_eq!(slugify("A"), "a");
        assert_eq!(slugify("-"), "");
        assert_eq!(slugify("5"), "5");
    }

    #[test]
    fn unicode_stripped() {
        // Non-ASCII chars are stripped (not transliterated)
        let slug = slugify("cafe\u{0301}");
        assert_eq!(slug, "caf");
    }

    // --- Snapshot tests ---

    #[test]
    fn slug_snapshots() {
        let cases = vec![
            ("Hello, World!", slugify("Hello, World!")),
            ("  Multiple   Spaces  ", slugify("  Multiple   Spaces  ")),
            ("CamelCaseTitle", slugify("CamelCaseTitle")),
            ("---already---slugged---", slugify("---already---slugged---")),
            ("Special!@#$%^&*()Characters", slugify("Special!@#$%^&*()Characters")),
        ];

        insta::assert_json_snapshot!(cases);
    }
}
```
</details>

### Exercise 2: Custom Strategies for Domain Types

Build a simplified order system and test it with custom proptest strategies. The order has constraints that generation must respect.

```toml
[package]
name = "domain-strategies"
version = "0.1.0"
edition = "2024"

[dev-dependencies]
proptest = "1.6"
```

**Requirements:**
- `Order { id: u64, items: Vec<LineItem>, discount_percent: u8 }` where `discount_percent <= 50`
- `LineItem { sku: String, quantity: u32, price_cents: u64 }` where `quantity >= 1`, `price_cents >= 1`
- `fn total_cents(order: &Order) -> u64`: sum of (quantity * price) for all items, minus discount
- Custom strategy: `order_strategy()` that generates valid orders (respects all constraints)
- Property 1 (invariant): total is always <= sum without discount
- Property 2 (oracle): compare against a reference implementation using f64 floats
- Property 3 (empty order): total of order with no items is 0 regardless of discount
- Property 4 (commutativity): reordering items does not change total
- Property 5 (monotonicity): adding an item never decreases the total

<details>
<summary>Solution</summary>

```rust
#[derive(Debug, Clone)]
struct LineItem {
    sku: String,
    quantity: u32,
    price_cents: u64,
}

#[derive(Debug, Clone)]
struct Order {
    id: u64,
    items: Vec<LineItem>,
    discount_percent: u8,
}

fn total_cents(order: &Order) -> u64 {
    let subtotal: u64 = order.items.iter()
        .map(|item| item.quantity as u64 * item.price_cents)
        .sum();
    let discount = subtotal * order.discount_percent as u64 / 100;
    subtotal - discount
}

/// Reference implementation using f64 (known correct but imprecise)
fn total_cents_reference(order: &Order) -> u64 {
    let subtotal: f64 = order.items.iter()
        .map(|item| item.quantity as f64 * item.price_cents as f64)
        .sum();
    let discount = subtotal * order.discount_percent as f64 / 100.0;
    (subtotal - discount).floor() as u64
}

fn main() {
    let order = Order {
        id: 1,
        items: vec![
            LineItem { sku: "A".into(), quantity: 2, price_cents: 1000 },
            LineItem { sku: "B".into(), quantity: 1, price_cents: 500 },
        ],
        discount_percent: 10,
    };
    println!("Total: {} cents", total_cents(&order));
}

#[cfg(test)]
mod tests {
    use super::*;
    use proptest::prelude::*;

    fn line_item_strategy() -> impl Strategy<Value = LineItem> {
        (
            "[A-Z]{2,6}",      // sku
            1..100u32,         // quantity >= 1
            1..100_000u64,     // price_cents >= 1
        ).prop_map(|(sku, quantity, price_cents)| LineItem {
            sku,
            quantity,
            price_cents,
        })
    }

    fn order_strategy() -> impl Strategy<Value = Order> {
        (
            any::<u64>(),                                          // id
            prop::collection::vec(line_item_strategy(), 0..20),    // items
            0..=50u8,                                              // discount_percent <= 50
        ).prop_map(|(id, items, discount_percent)| Order {
            id,
            items,
            discount_percent,
        })
    }

    fn non_empty_order_strategy() -> impl Strategy<Value = Order> {
        (
            any::<u64>(),
            prop::collection::vec(line_item_strategy(), 1..20),    // at least 1 item
            0..=50u8,
        ).prop_map(|(id, items, discount_percent)| Order {
            id,
            items,
            discount_percent,
        })
    }

    proptest! {
        #![proptest_config(ProptestConfig::with_cases(1000))]

        // Property 1: total <= subtotal (discount only reduces)
        #[test]
        fn total_lte_subtotal(order in order_strategy()) {
            let subtotal: u64 = order.items.iter()
                .map(|item| item.quantity as u64 * item.price_cents)
                .sum();
            let total = total_cents(&order);
            prop_assert!(
                total <= subtotal,
                "total {} > subtotal {} for discount {}%",
                total, subtotal, order.discount_percent
            );
        }

        // Property 2: matches reference implementation (within rounding)
        #[test]
        fn matches_reference(order in order_strategy()) {
            let fast = total_cents(&order);
            let reference = total_cents_reference(&order);
            // Allow difference of 1 cent due to integer vs float rounding
            let diff = if fast > reference { fast - reference } else { reference - fast };
            prop_assert!(
                diff <= 1,
                "fast={} reference={} diff={} for {:?}",
                fast, reference, diff, order
            );
        }

        // Property 3: empty order is always 0
        #[test]
        fn empty_order_is_zero(discount in 0..=50u8) {
            let order = Order {
                id: 0,
                items: vec![],
                discount_percent: discount,
            };
            prop_assert_eq!(total_cents(&order), 0);
        }

        // Property 4: item order does not matter
        #[test]
        fn commutativity(order in non_empty_order_strategy()) {
            let total_original = total_cents(&order);

            let mut reversed = order.clone();
            reversed.items.reverse();
            let total_reversed = total_cents(&reversed);

            prop_assert_eq!(total_original, total_reversed);
        }

        // Property 5: adding an item never decreases total
        #[test]
        fn monotonicity(
            order in order_strategy(),
            extra in line_item_strategy(),
        ) {
            let total_before = total_cents(&order);
            let mut bigger = order.clone();
            bigger.items.push(extra);
            let total_after = total_cents(&bigger);
            prop_assert!(
                total_after >= total_before,
                "total decreased from {} to {} after adding item",
                total_before, total_after,
            );
        }

        // Property 6: zero discount means total == subtotal
        #[test]
        fn zero_discount_is_subtotal(
            items in prop::collection::vec(line_item_strategy(), 0..10)
        ) {
            let order = Order {
                id: 0,
                items: items.clone(),
                discount_percent: 0,
            };
            let subtotal: u64 = items.iter()
                .map(|item| item.quantity as u64 * item.price_cents)
                .sum();
            prop_assert_eq!(total_cents(&order), subtotal);
        }
    }
}
```
</details>

### Exercise 3: Stateful Property Testing

Test a `BoundedStack<T>` (stack with a maximum capacity) using stateful property testing. Generate sequences of operations and verify invariants hold after every operation.

```toml
[package]
name = "stateful-properties"
version = "0.1.0"
edition = "2024"

[dev-dependencies]
proptest = "1.6"
```

**Requirements:**
- `BoundedStack::new(capacity)`, `.push(item) -> Result<(), StackFull>`, `.pop() -> Option<T>`, `.peek() -> Option<&T>`, `.len()`, `.is_empty()`, `.is_full()`
- Define an `Op` enum: `Push(i32)`, `Pop`, `Peek`
- Generate sequences of `Vec<Op>` with proptest
- After executing all ops, verify:
  - `len <= capacity` (invariant)
  - `is_empty() == (len == 0)` (consistency)
  - `is_full() == (len == capacity)` (consistency)
  - push on full returns `Err`, pop on empty returns `None`
- Run the same ops on a reference `Vec<i32>` and compare all results (oracle)

<details>
<summary>Solution</summary>

```rust
#[derive(Debug)]
struct StackFull;

#[derive(Debug)]
struct BoundedStack<T> {
    data: Vec<T>,
    capacity: usize,
}

impl<T> BoundedStack<T> {
    fn new(capacity: usize) -> Self {
        assert!(capacity > 0, "capacity must be > 0");
        BoundedStack {
            data: Vec::with_capacity(capacity),
            capacity,
        }
    }

    fn push(&mut self, item: T) -> Result<(), StackFull> {
        if self.data.len() >= self.capacity {
            return Err(StackFull);
        }
        self.data.push(item);
        Ok(())
    }

    fn pop(&mut self) -> Option<T> {
        self.data.pop()
    }

    fn peek(&self) -> Option<&T> {
        self.data.last()
    }

    fn len(&self) -> usize {
        self.data.len()
    }

    fn is_empty(&self) -> bool {
        self.data.is_empty()
    }

    fn is_full(&self) -> bool {
        self.data.len() == self.capacity
    }

    fn capacity(&self) -> usize {
        self.capacity
    }
}

fn main() {
    let mut stack = BoundedStack::new(3);
    stack.push(1).unwrap();
    stack.push(2).unwrap();
    stack.push(3).unwrap();
    assert!(stack.push(4).is_err());
    assert_eq!(stack.pop(), Some(3));
    assert_eq!(stack.pop(), Some(2));
    assert_eq!(stack.pop(), Some(1));
    assert_eq!(stack.pop(), None);
    println!("Basic operations verified.");
}

#[cfg(test)]
mod tests {
    use super::*;
    use proptest::prelude::*;

    #[derive(Debug, Clone)]
    enum Op {
        Push(i32),
        Pop,
        Peek,
    }

    fn op_strategy() -> impl Strategy<Value = Op> {
        prop_oneof![
            3 => any::<i32>().prop_map(Op::Push),  // weighted: more pushes
            2 => Just(Op::Pop),
            1 => Just(Op::Peek),
        ]
    }

    fn ops_strategy() -> impl Strategy<Value = Vec<Op>> {
        prop::collection::vec(op_strategy(), 0..100)
    }

    proptest! {
        #![proptest_config(ProptestConfig::with_cases(500))]

        #[test]
        fn invariant_len_le_capacity(
            capacity in 1..50usize,
            ops in ops_strategy(),
        ) {
            let mut stack = BoundedStack::new(capacity);
            for op in &ops {
                match op {
                    Op::Push(v) => { let _ = stack.push(*v); }
                    Op::Pop => { let _ = stack.pop(); }
                    Op::Peek => { let _ = stack.peek(); }
                }
                prop_assert!(
                    stack.len() <= stack.capacity(),
                    "len {} > capacity {} after {:?}",
                    stack.len(), stack.capacity(), op
                );
            }
        }

        #[test]
        fn consistency_is_empty(
            capacity in 1..50usize,
            ops in ops_strategy(),
        ) {
            let mut stack = BoundedStack::new(capacity);
            for op in &ops {
                match op {
                    Op::Push(v) => { let _ = stack.push(*v); }
                    Op::Pop => { let _ = stack.pop(); }
                    Op::Peek => { let _ = stack.peek(); }
                }
                prop_assert_eq!(
                    stack.is_empty(),
                    stack.len() == 0,
                    "is_empty() inconsistent with len()"
                );
            }
        }

        #[test]
        fn consistency_is_full(
            capacity in 1..50usize,
            ops in ops_strategy(),
        ) {
            let mut stack = BoundedStack::new(capacity);
            for op in &ops {
                match op {
                    Op::Push(v) => { let _ = stack.push(*v); }
                    Op::Pop => { let _ = stack.pop(); }
                    Op::Peek => { let _ = stack.peek(); }
                }
                prop_assert_eq!(
                    stack.is_full(),
                    stack.len() == stack.capacity(),
                    "is_full() inconsistent with len()/capacity()"
                );
            }
        }

        // Oracle: compare against Vec reference implementation
        #[test]
        fn oracle_matches_vec(
            capacity in 1..30usize,
            ops in ops_strategy(),
        ) {
            let mut stack = BoundedStack::new(capacity);
            let mut reference: Vec<i32> = Vec::new();

            for op in &ops {
                match op {
                    Op::Push(v) => {
                        let stack_result = stack.push(*v);
                        if reference.len() < capacity {
                            reference.push(*v);
                            prop_assert!(stack_result.is_ok());
                        } else {
                            prop_assert!(stack_result.is_err());
                        }
                    }
                    Op::Pop => {
                        let stack_val = stack.pop();
                        let ref_val = reference.pop();
                        prop_assert_eq!(stack_val, ref_val);
                    }
                    Op::Peek => {
                        let stack_val = stack.peek().copied();
                        let ref_val = reference.last().copied();
                        prop_assert_eq!(stack_val, ref_val);
                    }
                }
                prop_assert_eq!(stack.len(), reference.len());
            }
        }

        // Push on full always fails
        #[test]
        fn push_on_full_fails(
            capacity in 1..20usize,
            values in prop::collection::vec(any::<i32>(), 1..30),
        ) {
            let mut stack = BoundedStack::new(capacity);
            for &v in &values {
                let result = stack.push(v);
                if stack.len() <= capacity {
                    // Push happened or it was already full
                } else {
                    unreachable!("len should never exceed capacity");
                }
                // After push attempt, if result was Err, len should be capacity
                if result.is_err() {
                    prop_assert_eq!(stack.len(), capacity);
                }
            }
        }

        // Pop on empty always returns None
        #[test]
        fn pop_on_empty_returns_none(n_pops in 1..20usize) {
            let mut stack: BoundedStack<i32> = BoundedStack::new(10);
            for _ in 0..n_pops {
                prop_assert_eq!(stack.pop(), None);
                prop_assert!(stack.is_empty());
            }
        }
    }
}
```
</details>

### Exercise 4: Fuzz Target for a Parser

Build a simple expression parser and write both a proptest property test and a cargo-fuzz fuzz target for it. The parser handles `<number> <op> <number>` (e.g., `42 + 7`).

```toml
[package]
name = "expr-parser"
version = "0.1.0"
edition = "2024"

[dev-dependencies]
proptest = "1.6"
```

**Requirements:**
- Parse expressions of the form `"{number} {op} {number}"` where op is `+`, `-`, `*`, `/`
- Return `Result<i64, ParseError>` (handle overflow, division by zero)
- Property 1: any expression produced by the generator parses successfully
- Property 2: `format!("{a} + {b}")` parsed equals `a + b` (roundtrip from parts)
- Property 3: parsing invalid input never panics (returns Err)
- Write a fuzz target (as a separate file) that calls the parser with arbitrary bytes
- Demonstrate that cargo-fuzz would find division by zero if not handled

<details>
<summary>Solution</summary>

```rust
#[derive(Debug, PartialEq)]
enum Op {
    Add,
    Sub,
    Mul,
    Div,
}

#[derive(Debug, PartialEq)]
enum ParseError {
    InvalidFormat,
    InvalidNumber(String),
    UnknownOperator(String),
    DivisionByZero,
    Overflow,
}

impl std::fmt::Display for ParseError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::InvalidFormat => write!(f, "expected format: <number> <op> <number>"),
            Self::InvalidNumber(s) => write!(f, "invalid number: {s}"),
            Self::UnknownOperator(s) => write!(f, "unknown operator: {s}"),
            Self::DivisionByZero => write!(f, "division by zero"),
            Self::Overflow => write!(f, "arithmetic overflow"),
        }
    }
}

fn parse_op(s: &str) -> Result<Op, ParseError> {
    match s {
        "+" => Ok(Op::Add),
        "-" => Ok(Op::Sub),
        "*" => Ok(Op::Mul),
        "/" => Ok(Op::Div),
        other => Err(ParseError::UnknownOperator(other.to_string())),
    }
}

pub fn eval_expr(input: &str) -> Result<i64, ParseError> {
    let parts: Vec<&str> = input.trim().split_whitespace().collect();
    if parts.len() != 3 {
        return Err(ParseError::InvalidFormat);
    }

    let a: i64 = parts[0].parse()
        .map_err(|_| ParseError::InvalidNumber(parts[0].to_string()))?;
    let op = parse_op(parts[1])?;
    let b: i64 = parts[2].parse()
        .map_err(|_| ParseError::InvalidNumber(parts[2].to_string()))?;

    match op {
        Op::Add => a.checked_add(b).ok_or(ParseError::Overflow),
        Op::Sub => a.checked_sub(b).ok_or(ParseError::Overflow),
        Op::Mul => a.checked_mul(b).ok_or(ParseError::Overflow),
        Op::Div => {
            if b == 0 {
                Err(ParseError::DivisionByZero)
            } else {
                a.checked_div(b).ok_or(ParseError::Overflow)
            }
        }
    }
}

fn main() {
    let expressions = [
        "42 + 7",
        "100 - 30",
        "6 * 7",
        "84 / 2",
        "1 / 0",
        "9999999999999999999 + 1",
        "not a number",
    ];

    for expr in &expressions {
        println!("{expr:25} => {:?}", eval_expr(expr));
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use proptest::prelude::*;

    fn op_str_strategy() -> impl Strategy<Value = &'static str> {
        prop_oneof![
            Just("+"),
            Just("-"),
            Just("*"),
            Just("/"),
        ]
    }

    fn valid_expr_strategy() -> impl Strategy<Value = (i64, &'static str, i64)> {
        (
            -1_000_000i64..1_000_000,
            op_str_strategy(),
            // Avoid zero for division to test the happy path
            prop_oneof![
                -1_000_000i64..-1,
                1..1_000_000i64,
            ],
        )
    }

    proptest! {
        #![proptest_config(ProptestConfig::with_cases(2000))]

        // Property 1: generated valid expressions always parse
        #[test]
        fn valid_expressions_parse(
            (a, op, b) in valid_expr_strategy()
        ) {
            let expr = format!("{a} {op} {b}");
            let result = eval_expr(&expr);
            // Should not be InvalidFormat, InvalidNumber, or UnknownOperator
            match &result {
                Err(ParseError::InvalidFormat) => prop_assert!(false, "unexpected InvalidFormat"),
                Err(ParseError::InvalidNumber(_)) => prop_assert!(false, "unexpected InvalidNumber"),
                Err(ParseError::UnknownOperator(_)) => prop_assert!(false, "unexpected UnknownOperator"),
                _ => {} // Ok, DivisionByZero, or Overflow are acceptable
            }
        }

        // Property 2: roundtrip from components
        #[test]
        fn addition_roundtrip(a in -1000i64..1000, b in -1000i64..1000) {
            let expr = format!("{a} + {b}");
            let result = eval_expr(&expr).unwrap();
            prop_assert_eq!(result, a + b);
        }

        #[test]
        fn subtraction_roundtrip(a in -1000i64..1000, b in -1000i64..1000) {
            let expr = format!("{a} - {b}");
            let result = eval_expr(&expr).unwrap();
            prop_assert_eq!(result, a - b);
        }

        #[test]
        fn multiplication_roundtrip(a in -1000i64..1000, b in -1000i64..1000) {
            let expr = format!("{a} * {b}");
            let result = eval_expr(&expr).unwrap();
            prop_assert_eq!(result, a * b);
        }

        // Property 3: arbitrary input never panics
        #[test]
        fn never_panics(s in "\\PC{0,200}") {
            // We only care that this does not panic.
            // Any Result is fine.
            let _ = eval_expr(&s);
        }

        // Property 4: commutativity for + and *
        #[test]
        fn addition_commutative(a in -1000i64..1000, b in -1000i64..1000) {
            let r1 = eval_expr(&format!("{a} + {b}")).unwrap();
            let r2 = eval_expr(&format!("{b} + {a}")).unwrap();
            prop_assert_eq!(r1, r2);
        }

        #[test]
        fn multiplication_commutative(a in -1000i64..1000, b in -1000i64..1000) {
            let r1 = eval_expr(&format!("{a} * {b}")).unwrap();
            let r2 = eval_expr(&format!("{b} * {a}")).unwrap();
            prop_assert_eq!(r1, r2);
        }

        // Property 5: division by zero always returns DivisionByZero
        #[test]
        fn division_by_zero(a in any::<i64>()) {
            let result = eval_expr(&format!("{a} / 0"));
            prop_assert_eq!(result, Err(ParseError::DivisionByZero));
        }
    }

    // --- Example-based edge cases ---

    #[test]
    fn overflow_detected() {
        let result = eval_expr(&format!("{} + 1", i64::MAX));
        assert_eq!(result, Err(ParseError::Overflow));
    }

    #[test]
    fn underflow_detected() {
        let result = eval_expr(&format!("{} - 1", i64::MIN));
        assert_eq!(result, Err(ParseError::Overflow));
    }

    #[test]
    fn empty_input() {
        assert_eq!(eval_expr(""), Err(ParseError::InvalidFormat));
    }
}

// --- Fuzz target (would live in fuzz/fuzz_targets/eval_expr.rs) ---
// To use: cargo +nightly fuzz run eval_expr
//
// ```rust
// #![no_main]
// use libfuzzer_sys::fuzz_target;
//
// fuzz_target!(|data: &[u8]| {
//     if let Ok(s) = std::str::from_utf8(data) {
//         // Must never panic -- only return Ok or Err
//         let _ = expr_parser::eval_expr(s);
//     }
// });
// ```
```
</details>

### Exercise 5: Full Testing Pipeline

Create a `MarkdownToHtml` converter (simplified subset) and apply the complete testing pyramid: unit tests, property tests, snapshot tests, and mutation testing analysis.

```toml
[package]
name = "md-converter"
version = "0.1.0"
edition = "2024"

[dev-dependencies]
proptest = "1.6"
insta = "1.42"
```

**Requirements:**
- Support: `# heading`, `**bold**`, `*italic*`, `` `code` ``, blank lines as `<p>` breaks
- Property 1 (idempotency of normalization): normalizing whitespace twice equals once
- Property 2 (invariant): output always has balanced HTML tags
- Property 3 (roundtrip subset): headings roundtrip: `"# X"` -> `<h1>X</h1>` -> extract X
- Snapshot tests: 5 input documents with `insta::assert_snapshot!`
- Document which mutations `cargo-mutants` would catch and which it would miss
- Include a `justfile` recipe (or comment) showing how to run the full pipeline

<details>
<summary>Solution</summary>

```rust
/// Convert a simplified Markdown subset to HTML.
///
/// Supported syntax:
/// - `# Heading` -> `<h1>Heading</h1>` (up to ###)
/// - `**bold**` -> `<strong>bold</strong>`
/// - `*italic*` -> `<em>italic</em>`
/// - `` `code` `` -> `<code>code</code>`
/// - Blank lines separate paragraphs
pub fn md_to_html(input: &str) -> String {
    let mut output = String::new();
    let mut in_paragraph = false;

    for line in input.lines() {
        let trimmed = line.trim();

        if trimmed.is_empty() {
            if in_paragraph {
                output.push_str("</p>\n");
                in_paragraph = false;
            }
            continue;
        }

        // Headings
        if let Some(rest) = trimmed.strip_prefix("### ") {
            if in_paragraph {
                output.push_str("</p>\n");
                in_paragraph = false;
            }
            output.push_str(&format!("<h3>{}</h3>\n", inline_format(rest)));
            continue;
        }
        if let Some(rest) = trimmed.strip_prefix("## ") {
            if in_paragraph {
                output.push_str("</p>\n");
                in_paragraph = false;
            }
            output.push_str(&format!("<h2>{}</h2>\n", inline_format(rest)));
            continue;
        }
        if let Some(rest) = trimmed.strip_prefix("# ") {
            if in_paragraph {
                output.push_str("</p>\n");
                in_paragraph = false;
            }
            output.push_str(&format!("<h1>{}</h1>\n", inline_format(rest)));
            continue;
        }

        // Paragraph text
        if !in_paragraph {
            output.push_str("<p>");
            in_paragraph = true;
        } else {
            output.push(' ');
        }
        output.push_str(&inline_format(trimmed));
    }

    if in_paragraph {
        output.push_str("</p>\n");
    }

    output
}

fn inline_format(text: &str) -> String {
    let mut result = String::with_capacity(text.len() * 2);
    let chars: Vec<char> = text.chars().collect();
    let len = chars.len();
    let mut i = 0;

    while i < len {
        // Inline code: `...`
        if chars[i] == '`' {
            if let Some(end) = find_closing(&chars, i + 1, '`') {
                result.push_str("<code>");
                result.extend(&chars[i + 1..end]);
                result.push_str("</code>");
                i = end + 1;
                continue;
            }
        }

        // Bold: **...**
        if i + 1 < len && chars[i] == '*' && chars[i + 1] == '*' {
            if let Some(end) = find_closing_double(&chars, i + 2, '*') {
                result.push_str("<strong>");
                result.extend(&chars[i + 2..end]);
                result.push_str("</strong>");
                i = end + 2;
                continue;
            }
        }

        // Italic: *...*
        if chars[i] == '*' {
            if let Some(end) = find_closing(&chars, i + 1, '*') {
                result.push_str("<em>");
                result.extend(&chars[i + 1..end]);
                result.push_str("</em>");
                i = end + 1;
                continue;
            }
        }

        // HTML-escape special chars
        match chars[i] {
            '<' => result.push_str("&lt;"),
            '>' => result.push_str("&gt;"),
            '&' => result.push_str("&amp;"),
            c => result.push(c),
        }
        i += 1;
    }

    result
}

fn find_closing(chars: &[char], start: usize, marker: char) -> Option<usize> {
    for i in start..chars.len() {
        if chars[i] == marker {
            return Some(i);
        }
    }
    None
}

fn find_closing_double(chars: &[char], start: usize, marker: char) -> Option<usize> {
    for i in start..chars.len().saturating_sub(1) {
        if chars[i] == marker && chars[i + 1] == marker {
            return Some(i);
        }
    }
    None
}

/// Normalize whitespace in Markdown (helper for idempotency testing)
pub fn normalize_whitespace(input: &str) -> String {
    input
        .lines()
        .map(|line| {
            let trimmed = line.trim();
            if trimmed.is_empty() {
                String::new()
            } else {
                // Collapse multiple spaces to one
                trimmed.split_whitespace().collect::<Vec<_>>().join(" ")
            }
        })
        .collect::<Vec<_>>()
        .join("\n")
}

fn main() {
    let md = r#"# Hello World

This is a **bold** statement with *italic* words and `inline code`.

## Second Section

Another paragraph with **nested *formatting*** examples.

### Subsection

Final paragraph.
"#;

    println!("{}", md_to_html(md));
}

#[cfg(test)]
mod tests {
    use super::*;
    use proptest::prelude::*;

    // --- Property-based tests ---

    proptest! {
        #![proptest_config(ProptestConfig::with_cases(500))]

        // Property 1: whitespace normalization is idempotent
        #[test]
        fn normalize_idempotent(s in "[a-zA-Z0-9 \\n]{0,200}") {
            let once = normalize_whitespace(&s);
            let twice = normalize_whitespace(&once);
            prop_assert_eq!(once, twice);
        }

        // Property 2: output has balanced tags
        #[test]
        fn balanced_tags(s in "[a-zA-Z0-9 #*`\\n]{0,100}") {
            let html = md_to_html(&s);
            for (open, close) in [
                ("<h1>", "</h1>"),
                ("<h2>", "</h2>"),
                ("<h3>", "</h3>"),
                ("<p>", "</p>"),
                ("<strong>", "</strong>"),
                ("<em>", "</em>"),
                ("<code>", "</code>"),
            ] {
                let open_count = html.matches(open).count();
                let close_count = html.matches(close).count();
                prop_assert_eq!(
                    open_count, close_count,
                    "unbalanced {} ({} opens, {} closes) in html: {:?}\nfrom input: {:?}",
                    open, open_count, close_count, html, s
                );
            }
        }

        // Property 3: heading roundtrip
        #[test]
        fn heading_roundtrip(text in "[a-zA-Z0-9 ]{1,40}") {
            let md = format!("# {text}");
            let html = md_to_html(&md);
            // Extract content between <h1> and </h1>
            let start = html.find("<h1>").unwrap() + 4;
            let end = html.find("</h1>").unwrap();
            let extracted = &html[start..end];
            prop_assert_eq!(
                extracted, text.trim(),
                "heading roundtrip failed: md={:?} html={:?}", md, html
            );
        }

        // Property 4: HTML special chars are always escaped
        #[test]
        fn special_chars_escaped(s in "[a-zA-Z<>&]{1,50}") {
            let html = md_to_html(&s);
            // No raw < or > or & should appear outside of HTML tags we generate
            // Check that input < > & become &lt; &gt; &amp;
            for ch in s.chars() {
                match ch {
                    '<' => prop_assert!(html.contains("&lt;")),
                    '>' => prop_assert!(html.contains("&gt;")),
                    '&' => prop_assert!(html.contains("&amp;")),
                    _ => {}
                }
            }
        }

        // Property 5: output never panics
        #[test]
        fn never_panics(s in "\\PC{0,200}") {
            let _ = md_to_html(&s);
        }
    }

    // --- Snapshot tests ---

    #[test]
    fn snapshot_simple_heading() {
        insta::assert_snapshot!(md_to_html("# Hello"));
    }

    #[test]
    fn snapshot_paragraphs() {
        insta::assert_snapshot!(md_to_html("First paragraph.\n\nSecond paragraph."));
    }

    #[test]
    fn snapshot_inline_formatting() {
        insta::assert_snapshot!(md_to_html("This is **bold** and *italic* and `code`."));
    }

    #[test]
    fn snapshot_mixed_document() {
        let md = "# Title\n\nSome **text** here.\n\n## Subtitle\n\nMore *words*.";
        insta::assert_snapshot!(md_to_html(md));
    }

    #[test]
    fn snapshot_html_escaping() {
        insta::assert_snapshot!(md_to_html("Use <div> & \"quotes\" in text."));
    }

    // --- Example-based edge cases ---

    #[test]
    fn empty_input() {
        assert_eq!(md_to_html(""), "");
    }

    #[test]
    fn only_whitespace() {
        assert_eq!(md_to_html("   \n  \n   "), "");
    }

    #[test]
    fn unclosed_bold() {
        // Unclosed ** should be treated as literal
        let html = md_to_html("text **unclosed");
        assert!(html.contains("**unclosed"));
    }

    #[test]
    fn heading_without_space() {
        // "#nospc" is not a heading
        let html = md_to_html("#nospc");
        assert!(!html.contains("<h1>"));
    }

    // --- Mutation testing analysis ---
    //
    // Running `cargo mutants` on this module would test:
    //
    // CAUGHT mutations (tests detect the bug):
    // - Replacing `"<h1>"` with `""` -> heading_roundtrip fails
    // - Removing `</p>` push -> balanced_tags fails
    // - Replacing `&lt;` with `<` -> special_chars_escaped fails
    // - Changing `strip_prefix("# ")` to `strip_prefix("## ")` -> snapshot tests fail
    //
    // MISSED mutations (tests might not detect):
    // - Changing capacity hint in `String::with_capacity` -> functional, not observable
    // - Reordering heading checks (### before ## before #) -> same logic, same results
    //
    // Run the full pipeline:
    //   cargo test                           # unit + property + snapshot
    //   cargo llvm-cov --html                # coverage report
    //   cargo mutants --file src/main.rs     # mutation testing
    //   cargo insta review                   # review any snapshot changes
}
```
</details>

## Common Mistakes

1. **Testing implementation instead of properties.** `assert_eq!(sort(vec![3,1,2]), vec![1,2,3])` tests one case. `assert!(sorted.windows(2).all(|w| w[0] <= w[1]))` tests the property for all inputs.

2. **Overly constrained strategies.** If your strategy only generates ASCII alphanumeric strings, you miss bugs triggered by unicode, empty strings, or special characters. Start broad, narrow only when the function's contract demands it.

3. **Ignoring shrinking.** If proptest reports a 200-element vector as the failing case, your strategy might not shrink well. Prefer `prop_map` over `prop_flat_map` when possible, since flat_map limits shrinking.

4. **Confusing coverage with correctness.** `cargo-llvm-cov` says "100% coverage" but `cargo-mutants` reveals 15 missed mutations. Coverage measures execution, not assertion quality.

5. **Not persisting regressions.** proptest writes failing cases to `proptest-regressions/`. Commit this directory. It ensures the same failure is tested on every future run.

## Verification

```bash
# Property tests
cargo test

# Accept snapshots (first run)
cargo insta review

# Coverage report
cargo llvm-cov --html && open target/llvm-cov/html/index.html

# Mutation testing
cargo mutants --file src/main.rs

# Fuzzing (requires nightly)
cargo +nightly fuzz run eval_expr -- -max_total_time=60
```

## Summary

Property-based testing finds bugs that example-based tests miss. The five property categories (roundtrip, idempotency, invariant, commutativity, oracle) cover most testing needs. proptest generates and shrinks inputs automatically. cargo-fuzz adds coverage-guided exploration for parsers and unsafe code. insta captures complex outputs as reviewable snapshots. cargo-mutants verifies your assertions actually catch bugs. Together, these tools form a testing pipeline that is qualitatively stronger than hand-written unit tests alone.

## What's Next

Exercise 19 explores memory layout optimization -- understanding how Rust lays out types in memory and how to control padding, alignment, and niche optimization for performance-critical code.

## Resources

- [proptest documentation](https://docs.rs/proptest/1.6) -- strategies, config, shrinking
- [proptest book](https://proptest-rs.github.io/proptest/intro.html) -- comprehensive guide
- [insta documentation](https://insta.rs/) -- snapshot testing
- [cargo-fuzz book](https://rust-fuzz.github.io/book/) -- fuzzing guide
- [cargo-mutants](https://mutants.rs/) -- mutation testing
- [cargo-llvm-cov](https://github.com/taiki-e/cargo-llvm-cov) -- code coverage
- [Choosing Properties for Property-Based Testing](https://fsharpforfunandprofit.com/posts/property-based-testing-2/) -- Scott Wlaschin (F# but concepts transfer)
