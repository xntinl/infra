# Vectors

**Difficulty:** Basico
**Time:** 40-50 minutes
**Prerequisites:** Ownership, references, Option, iterators (basic exposure)

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** how to create vectors with `vec!` and `Vec::new`, and the difference between `[]` indexing and `.get()`.
- **Understand** how capacity, length, and reallocation work, and why borrowing rules apply to vector elements.
- **Apply** iteration patterns (`iter`, `iter_mut`, `into_iter`) and common methods to transform collections of data.

## Concepts

### Why vectors matter

Arrays in Rust have a fixed size known at compile time. That is fine when you know exactly how many elements you need. But most real programs deal with lists that grow and shrink: rows from a database, lines from a file, tasks in a queue. `Vec<T>` is Rust's growable, heap-allocated array. It is the collection you will reach for first in almost every Rust program.

### Creation

Two ways to create a vector:

```rust
// From a literal list of values
let nums = vec![1, 2, 3];

// Empty, then push
let mut names: Vec<String> = Vec::new();
names.push("Alice".to_string());
```

`vec!` is a macro that allocates and fills in one step. `Vec::new()` starts empty with zero capacity.

### Indexing vs .get()

Direct indexing with `[]` panics on out-of-bounds access. `.get()` returns an `Option`:

```rust
let v = vec![10, 20, 30];
println!("{}", v[1]);       // 20 -- panics if index >= v.len()
println!("{:?}", v.get(1)); // Some(20)
println!("{:?}", v.get(9)); // None -- no panic
```

Use `[]` when you are certain the index is valid (you just checked `len()`, or the logic guarantees it). Use `.get()` when the index comes from external input.

### Capacity vs length

Length is how many elements are in the vector. Capacity is how many it can hold before it needs to allocate more memory:

```rust
let mut v = Vec::with_capacity(10);
println!("len: {}, capacity: {}", v.len(), v.capacity()); // 0, 10
v.push(1);
println!("len: {}, capacity: {}", v.len(), v.capacity()); // 1, 10
```

When `push` would exceed capacity, the vector doubles its allocation and copies everything. This is O(1) amortized, but if you know the final size upfront, `Vec::with_capacity` avoids repeated reallocations.

### Iteration

Three ways to iterate, each with different ownership semantics:

- `v.iter()` -- yields `&T` (borrows immutably, vector still usable after).
- `v.iter_mut()` -- yields `&mut T` (borrows mutably, can modify elements in place).
- `v.into_iter()` -- yields `T` (consumes the vector, takes ownership of elements).

A `for` loop on `&v` calls `iter()` implicitly. A `for` loop on `v` calls `into_iter()`.

### Ownership rules with vectors

The borrow checker treats the whole vector as one value. You cannot hold a reference to an element and then push, because pushing might reallocate and invalidate all references:

```rust
let mut v = vec![1, 2, 3];
let first = &v[0];    // immutable borrow of v
v.push(4);             // ERROR: mutable borrow while immutable borrow is alive
println!("{}", first);
```

This is not arbitrary strictness. If `push` reallocates, `first` would point to freed memory. The compiler prevents a real use-after-free bug.

## Exercises

### Exercise 1 -- Create, push, access

What do you think this will print?

```rust
fn main() {
    let mut scores = vec![85, 92, 78];
    scores.push(95);
    scores.push(88);

    println!("Total scores: {}", scores.len());
    println!("First: {}", scores[0]);
    println!("Last: {:?}", scores.last());
    println!("Get index 2: {:?}", scores.get(2));
    println!("Get index 99: {:?}", scores.get(99));

    let removed = scores.pop();
    println!("Popped: {:?}", removed);
    println!("After pop: {:?}", scores);
}
```

Predict, then verify with `cargo run`.

### Exercise 2 -- Three iteration styles

Observe how each iteration style affects ownership:

```rust
fn main() {
    let names = vec![
        String::from("Alice"),
        String::from("Bob"),
        String::from("Charlie"),
    ];

    // iter() borrows -- names is still valid after
    print!("Greeting: ");
    for name in names.iter() {
        print!("Hi {}! ", name);
    }
    println!();

    // We can still use names because iter() only borrowed
    println!("Count: {}", names.len());

    // iter_mut() requires mut -- modifies in place
    let mut numbers = vec![1, 2, 3, 4, 5];
    for n in numbers.iter_mut() {
        *n *= 2;
    }
    println!("Doubled: {:?}", numbers);

    // into_iter() consumes -- names_copy is gone after the loop
    let names_copy = vec![
        String::from("Dave"),
        String::from("Eve"),
    ];
    let mut uppercased = Vec::new();
    for name in names_copy.into_iter() {
        uppercased.push(name.to_uppercase());
    }
    println!("Uppercased: {:?}", uppercased);

    // Uncomment the next line to see the ownership error:
    // println!("Original: {:?}", names_copy);
}
```

### Exercise 3 -- Common methods

Practice the utility methods you will use every day:

```rust
fn main() {
    // sort and dedup
    let mut vals = vec![3, 1, 4, 1, 5, 9, 2, 6, 5, 3, 5];
    vals.sort();
    println!("Sorted: {:?}", vals);
    vals.dedup();
    println!("Deduped: {:?}", vals);

    // retain -- keep only elements matching a predicate
    let mut even_only = vec![1, 2, 3, 4, 5, 6, 7, 8];
    even_only.retain(|&x| x % 2 == 0);
    println!("Evens: {:?}", even_only);

    // contains and position
    let fruits = vec!["apple", "banana", "cherry"];
    println!("Has banana: {}", fruits.contains(&"banana"));
    println!("Position of cherry: {:?}", fruits.iter().position(|&f| f == "cherry"));
    println!("Position of grape: {:?}", fruits.iter().position(|&f| f == "grape"));

    // extend and append
    let mut a = vec![1, 2, 3];
    let mut b = vec![4, 5, 6];
    a.append(&mut b); // moves all elements from b into a
    println!("a after append: {:?}", a);
    println!("b after append: {:?}", b); // b is now empty
}
```

Before running, predict: what does `dedup` do to a sorted list with duplicates? What happens to `b` after `append`?

### Exercise 4 -- Ownership pitfalls

This exercise demonstrates the borrow checker protecting you. Read, predict the error, then verify:

```rust
fn main() {
    let mut v = vec![1, 2, 3, 4, 5];

    // This works: borrow ends before mutation
    let third = v[2];
    v.push(6);
    println!("third: {}, v: {:?}", third, v);

    // This fails with String (non-Copy type):
    let mut words = vec![String::from("hello"), String::from("world")];

    // Uncomment to see the error:
    // let first = &words[0];
    // words.push(String::from("!"));
    // println!("{}", first);

    // Safe alternative: clone the value
    let first_clone = words[0].clone();
    words.push(String::from("!"));
    println!("Cloned: {}, words: {:?}", first_clone, words);

    // Safe alternative: use get() and finish with the reference before pushing
    if let Some(word) = words.get(0) {
        println!("Peeked: {}", word);
    }
    words.push(String::from("more"));
    println!("Final: {:?}", words);
}
```

Note: `i32` is `Copy`, so `let third = v[2]` copies the value and does not borrow the vector. That is why the first block compiles. `String` is not `Copy`, so `&words[0]` borrows the vector.

### Exercise 5 -- Building a frequency counter

Combine vectors with iteration to count word frequencies (a preview of HashMaps):

```rust
fn top_words(text: &str, n: usize) -> Vec<(String, usize)> {
    let words: Vec<&str> = text.split_whitespace().collect();
    let mut counts: Vec<(String, usize)> = Vec::new();

    for word in &words {
        let lower = word.to_lowercase();
        let mut found = false;
        for entry in counts.iter_mut() {
            if entry.0 == lower {
                entry.1 += 1;
                found = true;
                break;
            }
        }
        if !found {
            counts.push((lower, 1));
        }
    }

    counts.sort_by(|a, b| b.1.cmp(&a.1)); // sort descending by count
    counts.truncate(n);
    counts
}

fn main() {
    let text = "the cat sat on the mat and the cat sat";
    let top = top_words(text, 3);

    println!("Top 3 words:");
    for (word, count) in &top {
        println!("  {}: {}", word, count);
    }

    println!("\nAll words: {:?}", top_words(text, 100));
}
```

Predict the top 3 words and their counts before running.

## Common Mistakes

**Indexing out of bounds at runtime:**

```
thread 'main' panicked at 'index out of bounds: the len is 3 but the index is 5'
```

Fix: use `.get()` which returns `Option`, or check `v.len()` first.

**Pushing while holding a reference:**

```
error[E0502]: cannot borrow `v` as mutable because it is also borrowed as immutable
  --> src/main.rs:4:5
   |
3  |     let first = &v[0];
   |                  - immutable borrow occurs here
4  |     v.push(6);
   |     ^ mutable borrow occurs here
5  |     println!("{}", first);
   |                    ----- immutable borrow later used here
```

Fix: finish using the reference before mutating, or clone the value.

**Using into_iter() when you want to keep the vector:**

After `for item in vec { ... }` the vector is consumed. If you meant to borrow, use `for item in &vec` or `vec.iter()`.

## Verification

```bash
# Exercise 1 -- basic operations
cargo run

# Exercise 2 -- iteration ownership
cargo run

# Exercise 3 -- utility methods
cargo run

# Exercise 5 -- frequency counter
cargo run
```

For Exercise 4, uncomment the marked lines one at a time and confirm the compiler error matches the explanation above.

## Summary

- `Vec<T>` is a growable, heap-allocated list. Use `vec![]` or `Vec::new()` to create one.
- `[]` panics on bad indices; `.get()` returns `Option`.
- `iter()` borrows, `iter_mut()` borrows mutably, `into_iter()` consumes.
- Capacity and length are different; `with_capacity` avoids unnecessary reallocations.
- The borrow checker prevents holding a reference while pushing -- this protects you from real memory bugs.
- Useful methods: `push`, `pop`, `sort`, `dedup`, `retain`, `contains`, `append`.

## What's Next

Strings -- which are, under the hood, `Vec<u8>` with a UTF-8 guarantee. Understanding vectors makes strings much easier to reason about.

## Resources

- [The Rust Book -- Vectors](https://doc.rust-lang.org/book/ch08-01-vectors.html)
- [std::vec::Vec](https://doc.rust-lang.org/std/vec/struct.Vec.html)
- [Rust By Example -- Vectors](https://doc.rust-lang.org/rust-by-example/std/vec.html)
