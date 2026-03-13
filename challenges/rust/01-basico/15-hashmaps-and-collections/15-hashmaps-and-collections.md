# HashMaps and Collections

**Difficulty:** Basico
**Time:** 45-55 minutes
**Prerequisites:** Vectors, strings, ownership, Option

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** how to create a `HashMap`, insert entries, and retrieve values with `.get()`.
- **Understand** the entry API (`or_insert`, `and_modify`) and how key ownership works.
- **Apply** the right collection type (`HashMap`, `BTreeMap`, `HashSet`, `BTreeSet`) for a given problem.

## Concepts

### Why you need more than vectors

Vectors give you ordered, indexed access. But when you need to look up a value by a key -- a username, a config setting, an error code -- linear search through a vector is O(n). A hash map gives you O(1) average lookup by hashing the key into a bucket. This is the difference between "scan every row" and "jump straight to the answer."

### HashMap creation and insertion

`HashMap` lives in `std::collections`, not the prelude, so you must import it:

```rust
use std::collections::HashMap;

let mut scores: HashMap<String, i32> = HashMap::new();
scores.insert(String::from("Alice"), 95);
scores.insert(String::from("Bob"), 87);
```

You can also build one from an iterator of tuples:

```rust
let data = vec![("Alice", 95), ("Bob", 87)];
let scores: HashMap<&str, i32> = data.into_iter().collect();
```

### .get() returns Option

Because a key might not exist, `.get()` returns `Option<&V>`:

```rust
let alice: Option<&i32> = scores.get("Alice");
let unknown: Option<&i32> = scores.get("Charlie");
```

This forces you to handle the missing case. No null pointer surprises.

### The entry API

The entry API is the idiomatic way to insert-if-absent or update-in-place without doing two lookups:

```rust
// Insert only if the key does not already exist
scores.entry(String::from("Charlie")).or_insert(70);

// Insert a default and get a mutable reference to modify it
let count = word_counts.entry(word).or_insert(0);
*count += 1;

// Modify an existing value or insert a new one
scores.entry(String::from("Alice"))
    .and_modify(|s| *s += 10)
    .or_insert(0);
```

`or_insert` returns `&mut V` -- a mutable reference to the value, whether it was just inserted or already existed. This pattern is the bread and butter of counting, grouping, and accumulation.

### Key ownership

When you insert a `String` key, the `HashMap` takes ownership of it:

```rust
let name = String::from("Alice");
scores.insert(name, 95);
// name is moved -- cannot use it here
```

If your keys are `&str` references, they must outlive the map. In practice, you usually use `String` keys for maps that own their data, and `&str` keys for short-lived lookups on data that lives elsewhere.

### Other collection types

- **BTreeMap** -- like `HashMap` but keys are sorted. Use it when you need ordered iteration or range queries. O(log n) lookups instead of O(1).
- **HashSet** -- a `HashMap<K, ()>`. Stores unique keys with no associated values. Use it for membership testing ("have I seen this before?").
- **BTreeSet** -- sorted version of `HashSet`. Iteration yields elements in order.

Choosing the right one:
- Need key-value lookup? `HashMap` (unordered) or `BTreeMap` (sorted).
- Need unique membership? `HashSet` (unordered) or `BTreeSet` (sorted).
- Need order of insertion? Use a `Vec` of tuples, or the `indexmap` crate.

## Exercises

### Exercise 1 -- Basic HashMap operations

What do you think this will print?

```rust
use std::collections::HashMap;

fn main() {
    let mut capitals: HashMap<&str, &str> = HashMap::new();
    capitals.insert("France", "Paris");
    capitals.insert("Japan", "Tokyo");
    capitals.insert("Brazil", "Brasilia");

    println!("Count: {}", capitals.len());
    println!("France: {:?}", capitals.get("France"));
    println!("Germany: {:?}", capitals.get("Germany"));

    // Overwrite an existing key
    capitals.insert("Brazil", "Brasilia (updated)");
    println!("Brazil: {:?}", capitals.get("Brazil"));

    // Iterate -- order is NOT guaranteed
    println!("\nAll capitals:");
    for (country, city) in &capitals {
        println!("  {} -> {}", country, city);
    }

    // Remove
    let removed = capitals.remove("Japan");
    println!("\nRemoved Japan: {:?}", removed);
    println!("Count after remove: {}", capitals.len());
}
```

Predict the output. Note that iteration order for `HashMap` is non-deterministic -- your output may list countries in a different order.

### Exercise 2 -- Word frequency counter with entry API

The classic use case for the entry API:

```rust
use std::collections::HashMap;

fn word_frequencies(text: &str) -> Vec<(String, usize)> {
    let mut counts: HashMap<String, usize> = HashMap::new();

    for word in text.split_whitespace() {
        let lower = word.to_lowercase();
        let cleaned: String = lower.chars().filter(|c| c.is_alphanumeric()).collect();
        if !cleaned.is_empty() {
            *counts.entry(cleaned).or_insert(0) += 1;
        }
    }

    let mut result: Vec<(String, usize)> = counts.into_iter().collect();
    result.sort_by(|a, b| b.1.cmp(&a.1).then(a.0.cmp(&b.0)));
    result
}

fn main() {
    let text = "the cat sat on the mat and the cat sat on the hat";
    let freqs = word_frequencies(text);

    println!("Word frequencies:");
    for (word, count) in &freqs {
        println!("  {:>8}: {}", word, count);
    }

    println!("\nMost common: \"{}\" ({} times)", freqs[0].0, freqs[0].1);
}
```

Before running, count the occurrences of "the" and "cat" manually and verify your prediction.

### Exercise 3 -- entry API with and_modify

Use `and_modify` to update existing entries differently from new ones:

```rust
use std::collections::HashMap;

#[derive(Debug)]
struct Stats {
    count: u32,
    total: f64,
}

impl Stats {
    fn average(&self) -> f64 {
        if self.count == 0 { 0.0 } else { self.total / self.count as f64 }
    }
}

fn main() {
    let measurements = vec![
        ("cpu", 72.5),
        ("mem", 45.0),
        ("cpu", 68.3),
        ("disk", 90.1),
        ("cpu", 74.8),
        ("mem", 52.3),
    ];

    let mut stats: HashMap<&str, Stats> = HashMap::new();

    for (name, value) in &measurements {
        stats.entry(name)
            .and_modify(|s| {
                s.count += 1;
                s.total += value;
            })
            .or_insert(Stats { count: 1, total: *value });
    }

    println!("Metric averages:");
    for (name, s) in &stats {
        println!("  {}: {:.1} (from {} samples)", name, s.average(), s.count);
    }
}
```

Trace through the measurements and predict the count and average for "cpu" before running.

### Exercise 4 -- HashSet for membership

Use a `HashSet` to find duplicates and compute set operations:

```rust
use std::collections::HashSet;

fn main() {
    // Find duplicates in a list
    let ids = vec![1, 5, 3, 2, 5, 8, 3, 1, 9];
    let mut seen = HashSet::new();
    let mut duplicates = Vec::new();
    for id in &ids {
        if !seen.insert(id) {
            // insert returns false if the value was already present
            duplicates.push(*id);
        }
    }
    println!("Duplicates: {:?}", duplicates);
    println!("Unique count: {}", seen.len());

    // Set operations
    let team_a: HashSet<&str> = ["Alice", "Bob", "Charlie", "Dave"].into();
    let team_b: HashSet<&str> = ["Charlie", "Dave", "Eve", "Frank"].into();

    let both: HashSet<&&str> = team_a.intersection(&team_b).collect();
    let either: HashSet<&&str> = team_a.union(&team_b).collect();
    let only_a: HashSet<&&str> = team_a.difference(&team_b).collect();

    println!("\nTeam A: {:?}", team_a);
    println!("Team B: {:?}", team_b);
    println!("In both: {:?}", both);
    println!("In either: {:?}", either);
    println!("Only in A: {:?}", only_a);
}
```

Before running, work out the intersection and difference by hand.

### Exercise 5 -- BTreeMap for sorted output

Compare `HashMap` (unordered) with `BTreeMap` (sorted by key):

```rust
use std::collections::{BTreeMap, HashMap};

fn main() {
    let data = vec![
        ("zebra", 1),
        ("apple", 5),
        ("mango", 3),
        ("banana", 2),
        ("cherry", 4),
    ];

    // HashMap -- iteration order is unpredictable
    let hash: HashMap<&str, i32> = data.iter().cloned().collect();
    println!("HashMap iteration:");
    for (k, v) in &hash {
        println!("  {} = {}", k, v);
    }

    // BTreeMap -- iteration is always sorted by key
    let btree: BTreeMap<&str, i32> = data.iter().cloned().collect();
    println!("\nBTreeMap iteration:");
    for (k, v) in &btree {
        println!("  {} = {}", k, v);
    }

    // Range queries -- only BTreeMap supports this
    println!("\nFruits from 'b' to 'm':");
    for (k, v) in btree.range("banana"..="mango") {
        println!("  {} = {}", k, v);
    }

    // BTreeMap also has the entry API
    let mut counts: BTreeMap<char, usize> = BTreeMap::new();
    for ch in "hello world".chars() {
        if ch != ' ' {
            *counts.entry(ch).or_insert(0) += 1;
        }
    }
    println!("\nChar counts (sorted):");
    for (ch, n) in &counts {
        println!("  '{}': {}", ch, n);
    }
}
```

Predict the BTreeMap iteration order (alphabetical by key) and the range query results.

## Common Mistakes

**Forgetting to import HashMap:**

```
error[E0433]: failed to resolve: use of undeclared type `HashMap`
```

Fix: add `use std::collections::HashMap;` at the top of the file.

**Using a moved String key after insertion:**

```rust
let key = String::from("name");
map.insert(key, "value");
println!("{}", key); // ERROR: key was moved
```

Fix: clone the key before inserting if you still need it, or use `&str` keys if the data outlives the map.

**Expecting deterministic iteration order from HashMap:**

HashMap iteration order is random and can change between runs. If you need sorted output, either collect into a Vec and sort it, or use BTreeMap.

**Dereferencing the entry API result incorrectly:**

```rust
// Wrong -- forgot the *
counts.entry(word).or_insert(0) += 1;
```

```
error[E0368]: binary assignment operation `+=` cannot be applied to type `&mut usize`
```

Fix: dereference the mutable reference: `*counts.entry(word).or_insert(0) += 1;`

## Verification

```bash
# Exercise 1 -- basic operations
cargo run

# Exercise 2 -- word frequency (the core exercise)
cargo run

# Exercise 3 -- and_modify pattern
cargo run

# Exercise 4 -- HashSet operations
cargo run

# Exercise 5 -- BTreeMap vs HashMap
cargo run
```

Run Exercise 1 multiple times and observe that `HashMap` iteration order may differ between runs. Run Exercise 5 and confirm that `BTreeMap` order is always alphabetical.

## Summary

- `HashMap<K, V>` gives O(1) average key-value lookup. Import it from `std::collections`.
- `.get()` returns `Option<&V>` -- no null surprises.
- The entry API (`or_insert`, `and_modify`) is the idiomatic way to insert-or-update without double lookups.
- `String` keys are moved into the map. Use `&str` keys only when the data outlives the map.
- `BTreeMap` keeps keys sorted and supports range queries. Use it when order matters.
- `HashSet` and `BTreeSet` store unique values with no associated data -- use them for membership and set operations.

## What's Next

With structs, enums, Option, Result, vectors, strings, and hash maps covered, you have the core data modeling toolkit of Rust. The next level introduces error handling patterns, traits, generics, and lifetimes -- the features that make Rust's type system truly powerful.

## Resources

- [The Rust Book -- HashMaps](https://doc.rust-lang.org/book/ch08-03-hash-maps.html)
- [std::collections::HashMap](https://doc.rust-lang.org/std/collections/struct.HashMap.html)
- [std::collections::BTreeMap](https://doc.rust-lang.org/std/collections/struct.BTreeMap.html)
- [std::collections::HashSet](https://doc.rust-lang.org/std/collections/struct.HashSet.html)
- [std::collections module overview](https://doc.rust-lang.org/std/collections/index.html)
