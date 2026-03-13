# 5. Iterators

**Difficulty**: Intermedio

## Prerequisites
- Completed: 01-basico exercises (ownership, borrowing, vectors)
- Completed: 02-intermedio/01-traits (implementing traits)
- Completed: 02-intermedio/04-closures (closure syntax, Fn traits)

## Learning Objectives
After completing this exercise, you will be able to:
- Use the Iterator trait and its `next` method
- Choose between `iter()`, `iter_mut()`, and `into_iter()`
- Chain iterator adaptors (map, filter, enumerate, zip, flat_map) into pipelines
- Consume iterators with collect, sum, fold, find, and friends
- Implement Iterator for custom types
- Explain lazy evaluation and why iterator chains are zero-cost

## Concepts

### The Iterator Trait

At its core, an iterator is anything that implements one method:

```rust
pub trait Iterator {
    type Item;
    fn next(&mut self) -> Option<Self::Item>;
}
```

That's it. Call `next()` and you get `Some(value)` until the iterator is exhausted, then `None`. Everything else — `map`, `filter`, `collect` — is built on top of this single method.

### Three Ways to Iterate

Rust gives you control over ownership when iterating:

| Method | Yields | Original collection |
|--------|--------|-------------------|
| `.iter()` | `&T` | Preserved |
| `.iter_mut()` | `&mut T` | Preserved (mutated) |
| `.into_iter()` | `T` | Consumed |

This is the same ownership model you already know, applied to iteration. A `for` loop calls `into_iter()` by default.

### Lazy Evaluation

Iterator adaptors like `map` and `filter` don't do anything until consumed. They build a chain of operations that executes when you call a consuming adaptor like `collect`:

```rust
let numbers = vec![1, 2, 3, 4, 5];

// Nothing happens here — just sets up the pipeline
let pipeline = numbers.iter()
    .filter(|&&x| x > 2)
    .map(|&x| x * 10);

// NOW it runs — collect drives the whole chain
let result: Vec<i32> = pipeline.collect();
// [30, 40, 50]
```

This is why iterator chains are fast — there's no intermediate allocation. Each element flows through the entire chain, one at a time.

### Key Adaptors (Lazy — Return New Iterators)

- `map(|x| ...)` — transform each element
- `filter(|x| ...)` — keep elements where predicate is true
- `enumerate()` — yield `(index, value)` pairs
- `zip(other)` — pair elements from two iterators
- `chain(other)` — concatenate two iterators
- `take(n)` — yield only the first n elements
- `skip(n)` — skip the first n elements
- `flat_map(|x| ...)` — map, then flatten nested iterators

### Key Consumers (Eager — Produce a Final Value)

- `collect()` — gather into a collection
- `sum()` — add all elements
- `fold(init, |acc, x| ...)` — reduce to a single value
- `any(|x| ...)` — true if any element matches
- `all(|x| ...)` — true if all elements match
- `find(|x| ...)` — first element matching predicate
- `position(|x| ...)` — index of first match
- `count()` — number of elements

## Exercises

### Exercise 1: Basic Iterator Operations

```rust
fn main() {
    let numbers = vec![1, 2, 3, 4, 5, 6, 7, 8, 9, 10];

    // TODO: Use iterators to compute each of these. One line each.

    // 1. Sum of all numbers
    // let total: i32 = ...;
    // println!("Sum: {}", total); // 55

    // 2. A Vec of only the even numbers
    // let evens: Vec<&i32> = ...;
    // println!("Evens: {:?}", evens); // [2, 4, 6, 8, 10]

    // 3. A Vec of each number squared
    // let squares: Vec<i32> = ...;
    // println!("Squares: {:?}", squares); // [1, 4, 9, 16, 25, 36, 49, 64, 81, 100]

    // 4. The product of all numbers (use fold)
    // let product: i32 = ...;
    // println!("Product: {}", product); // 3628800

    // 5. Does any number exceed 7?
    // let has_big: bool = ...;
    // println!("Has big: {}", has_big); // true

    // 6. Are all numbers positive?
    // let all_positive: bool = ...;
    // println!("All positive: {}", all_positive); // true

    // 7. Find the first number greater than 5
    // let first_big: Option<&i32> = ...;
    // println!("First > 5: {:?}", first_big); // Some(6)
}
```

### Exercise 2: Chaining Adaptors

```rust
fn main() {
    let words = vec!["hello", "world", "rust", "is", "great"];

    // TODO: Use a single iterator chain for each.

    // 1. Uppercase all words and collect into Vec<String>
    // Hint: .map(|w| w.to_uppercase())
    // let upper: Vec<String> = ...;
    // println!("{:?}", upper); // ["HELLO", "WORLD", "RUST", "IS", "GREAT"]

    // 2. Words with more than 3 characters, with their index
    // Hint: enumerate first, then filter
    // let long_words: Vec<(usize, &&str)> = ...;
    // println!("{:?}", long_words); // [(0, "hello"), (1, "world"), (2, "rust"), (4, "great")]

    // 3. Join first 3 words with " - "
    // Hint: take(3), collect into Vec, then .join()
    // let joined: String = ...;
    // println!("{}", joined); // "hello - world - rust"

    // 4. Zip with numbers and format pairs
    let scores = vec![90, 85, 95, 70, 88];
    // let report: Vec<String> = words.iter()
    //     .zip(scores.iter())
    //     .map(|(word, score)| format!("{}: {}", word, score))
    //     .collect();
    // println!("{:?}", report); // ["hello: 90", "world: 85", "rust: 95", "is: 70", "great: 88"]

    // 5. Flatten nested vectors
    let nested = vec![vec![1, 2], vec![3, 4, 5], vec![6]];
    // let flat: Vec<&i32> = ...;
    // Hint: .iter().flat_map(|inner| inner.iter())
    // println!("{:?}", flat); // [1, 2, 3, 4, 5, 6]
}
```

### Try It Yourself

Given a `Vec<String>` of sentences, use a single iterator chain to:
1. Split each sentence into words
2. Filter words longer than 3 characters
3. Convert to lowercase
4. Collect into a `Vec<String>`

Hint: `flat_map` handles the "split then flatten" step.

### Exercise 3: iter vs iter_mut vs into_iter

```rust
fn main() {
    // --- iter(): borrows, original preserved ---
    let names = vec![
        String::from("Alice"),
        String::from("Bob"),
        String::from("Charlie"),
    ];

    let lengths: Vec<usize> = names.iter().map(|s| s.len()).collect();
    println!("Lengths: {:?}", lengths);
    println!("Names still here: {:?}", names); // names is NOT consumed

    // --- iter_mut(): mutable borrows ---
    let mut scores = vec![85, 90, 78, 92, 88];

    // TODO: Use iter_mut() to add 5 bonus points to every score
    // (modify in place, don't collect into a new vec)

    println!("Curved scores: {:?}", scores); // [90, 95, 83, 97, 93]

    // --- into_iter(): takes ownership ---
    let data = vec![
        String::from("hello"),
        String::from("world"),
    ];

    let shouted: Vec<String> = data.into_iter()
        .map(|s| s.to_uppercase())
        .collect();

    println!("Shouted: {:?}", shouted);
    // TODO: Try printing `data` here. What happens? Why?
}
```

### Exercise 4: Implementing Iterator for a Custom Type

```rust
struct Countdown {
    value: i32,
}

impl Countdown {
    fn new(start: i32) -> Self {
        Countdown { value: start }
    }
}

// TODO: Implement Iterator for Countdown.
// - Item type is i32
// - next() should return Some(current_value) then decrement
// - When value reaches 0, return Some(0), then None for all subsequent calls
//
// Example: Countdown::new(3) yields 3, 2, 1, 0, None, None, ...

fn main() {
    // Manual usage
    let mut c = Countdown::new(3);
    println!("{:?}", c.next()); // Some(3)
    println!("{:?}", c.next()); // Some(2)
    println!("{:?}", c.next()); // Some(1)
    println!("{:?}", c.next()); // Some(0)
    println!("{:?}", c.next()); // None

    // With a for loop (for loop calls into_iter, which for iterators is self)
    for n in Countdown::new(5) {
        print!("{} ", n);
    }
    println!(); // "5 4 3 2 1 0 "

    // With iterator adaptors — because it's an Iterator, everything works
    let sum: i32 = Countdown::new(10).sum();
    println!("Sum 10 to 0: {}", sum); // 55

    let evens: Vec<i32> = Countdown::new(10)
        .filter(|n| n % 2 == 0)
        .collect();
    println!("Even countdown: {:?}", evens); // [10, 8, 6, 4, 2, 0]
}
```

### Exercise 5: Real-World Iterator Pipeline

This pulls everything together — closures, iterator adaptors, and consuming adaptors.

```rust
#[derive(Debug)]
struct LogEntry {
    level: String,
    message: String,
    timestamp: u64,
}

fn make_logs() -> Vec<LogEntry> {
    vec![
        LogEntry { level: "INFO".into(), message: "Server started".into(), timestamp: 1000 },
        LogEntry { level: "ERROR".into(), message: "Connection refused".into(), timestamp: 1001 },
        LogEntry { level: "INFO".into(), message: "Request received".into(), timestamp: 1002 },
        LogEntry { level: "WARN".into(), message: "Slow query detected".into(), timestamp: 1003 },
        LogEntry { level: "ERROR".into(), message: "Timeout".into(), timestamp: 1004 },
        LogEntry { level: "INFO".into(), message: "Request received".into(), timestamp: 1005 },
        LogEntry { level: "ERROR".into(), message: "Disk full".into(), timestamp: 1006 },
        LogEntry { level: "INFO".into(), message: "Cleanup complete".into(), timestamp: 1007 },
    ]
}

fn main() {
    let logs = make_logs();

    // TODO 1: Count how many ERROR entries there are.
    // let error_count = ...;
    // println!("Errors: {}", error_count); // 3

    // TODO 2: Collect all ERROR messages into a Vec<&str>.
    // let error_messages: Vec<&str> = ...;
    // println!("Error messages: {:?}", error_messages);
    // ["Connection refused", "Timeout", "Disk full"]

    // TODO 3: Find the timestamp of the first WARN entry.
    // let first_warn_time: Option<u64> = ...;
    // println!("First warning at: {:?}", first_warn_time); // Some(1003)

    // TODO 4: Create a summary string for the last 3 log entries,
    // formatted as "[LEVEL] message" joined by newlines.
    // Hint: skip(logs.len() - 3), map to format, collect into Vec, join
    // let summary: String = ...;
    // println!("Recent:\n{}", summary);
    // [ERROR] Disk full
    // [INFO] Cleanup complete
    // ... (last 3)

    // TODO 5: Use fold to build a HashMap<String, usize> counting entries per level.
    // use std::collections::HashMap;
    // let counts: HashMap<String, usize> = logs.iter().fold(
    //     HashMap::new(),
    //     |mut acc, entry| {
    //         // TODO: increment the count for entry.level
    //         acc
    //     },
    // );
    // println!("Counts: {:?}", counts);
    // {"INFO": 4, "ERROR": 3, "WARN": 1}
}
```

## Common Mistakes

### Mistake 1: Forgetting That Iterators Are Lazy

```rust
let v = vec![1, 2, 3];
v.iter().map(|x| {
    println!("{}", x); // Never prints!
    x * 2
});
```

Nothing happens because `map` returns a lazy iterator. You must consume it:

```rust
let _: Vec<_> = v.iter().map(|x| {
    println!("{}", x);
    x * 2
}).collect(); // NOW it runs
```

Or use `for_each` if you only care about side effects:

```rust
v.iter().for_each(|x| println!("{}", x));
```

### Mistake 2: Confusing &T and T in Closures

```rust
let numbers = vec![1, 2, 3];
let doubled: Vec<i32> = numbers.iter()
    .map(|x| x * 2) // x is &i32 here — works because of auto-deref
    .collect();

// But filter is different:
let evens: Vec<&i32> = numbers.iter()
    .filter(|x| *x % 2 == 0) // x is &&i32 — need double deref or **x
    .collect();
```

Why? `iter()` yields `&i32`. `filter` takes `&&Item` because it borrows from the iterator. So you end up with `&&i32`. This trips up everyone at first — use `|&&x|` pattern destructuring or `**x` to deal with it:

```rust
let evens: Vec<&i32> = numbers.iter()
    .filter(|&&x| x % 2 == 0) // destructure both references
    .collect();
```

### Mistake 3: Using an Iterator After Consuming It

```rust
let nums = vec![1, 2, 3];
let iter = nums.iter();
let sum: i32 = iter.sum(); // consumes the iterator
let count = iter.count();  // ERROR — iter is gone
```

**Error**: `use of moved value: 'iter'`

**Fix**: Create a new iterator, or collect first and compute from the collection.

## Verification

```bash
cargo run
```

For each exercise:
1. Verify output matches the comments.
2. In Exercise 1, try implementing fold-based sum manually and compare with `.sum()`.
3. In Exercise 4, verify that `Countdown::new(0)` yields just `Some(0)` then `None`.
4. In Exercise 5, try replacing the fold with a for loop — notice how the iterator version avoids mutable state outside the closure.

## Summary

The Iterator trait is built on a single method: `next`. Adaptors like `map`, `filter`, and `flat_map` are lazy — they compose a pipeline that runs only when consumed by `collect`, `sum`, `fold`, etc. Rust's ownership model gives you `iter()`, `iter_mut()`, and `into_iter()` for different access patterns. Custom types can implement Iterator by defining `next`, getting the full adaptor ecosystem for free. Thanks to monomorphization, iterator chains compile down to code as fast as hand-written loops.

## What's Next

With traits, generics, lifetimes, closures, and iterators under your belt, you have the core intermediate toolkit. The next level covers error handling patterns, smart pointers, and concurrency.

## Resources

- [The Rust Book — Iterators](https://doc.rust-lang.org/book/ch13-02-iterators.html)
- [Iterator API docs](https://doc.rust-lang.org/std/iter/trait.Iterator.html)
- [Rust by Example — Iterators](https://doc.rust-lang.org/rust-by-example/trait/iter.html)
