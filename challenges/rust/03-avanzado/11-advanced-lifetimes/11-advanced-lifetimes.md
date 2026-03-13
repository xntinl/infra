# 11. Advanced Lifetimes

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-10 (ownership, borrowing, basic lifetimes, advanced traits)
- Comfortable with lifetime annotations on functions, structs, and impl blocks
- Familiarity with trait objects and closure syntax

## Learning Objectives

- Apply higher-ranked trait bounds (`for<'a>`) to express "works for any lifetime"
- Analyze lifetime variance (covariance, contravariance, invariance) and predict compile errors
- Distinguish subtyping from conversion in the lifetime system
- Debug lifetime elision edge cases in method signatures and trait impls
- Use GAT lifetime patterns for lending iterators and self-referential access

## Concepts

### Lifetime Subtyping: 'long: 'short

A lifetime `'a` is a subtype of `'b` if `'a` outlives `'b`. Written `'a: 'b`, this means any reference valid for `'a` is also valid for `'b`:

```rust
fn longest<'a, 'b>(x: &'a str, y: &'b str) -> &'a str
where
    'a: 'b, // 'a outlives 'b -- not actually needed here, just illustration
{
    if x.len() >= y.len() { x } else {
        // Can't return y here: y only lives for 'b, but we promised 'a
        x
    }
}
```

In practice, you rarely write `'a: 'b` explicitly. The compiler infers subtyping from how references flow. But understanding it is critical for debugging complex lifetime errors.

### Variance

Variance describes how a generic type's lifetime parameter relates to subtyping:

| Variance | Meaning | Example |
|---|---|---|
| **Covariant** | If `'a: 'b`, then `T<'a>` is a subtype of `T<'b>` | `&'a T`, `Box<&'a T>` |
| **Contravariant** | If `'a: 'b`, then `T<'b>` is a subtype of `T<'a>` | `fn(&'a T)` (in argument position) |
| **Invariant** | No subtyping relationship | `&'a mut T`, `Cell<&'a T>` |

This is why `&mut T` is more restrictive than `&T`:

```rust
fn covariant_demo() {
    let long_lived = String::from("hello");

    let r: &str = &long_lived;  // 'long coerced to 'short -- OK, covariant

    // &mut is invariant in its lifetime:
    let mut s = String::from("hi");
    let r1: &mut String = &mut s;
    // You cannot "shrink" the lifetime of &mut T and still use the original,
    // because that would create two &mut to the same data.
}
```

**Invariance in practice:** `Cell<&'a T>` is invariant in `'a` because if you could shrink the lifetime, you could write a short-lived reference into the cell and then read it back as long-lived -- a dangling reference.

### Reborrowing

When you pass `&mut T` to a function expecting `&mut T`, Rust does not move the mutable reference. It creates a temporary reborrow:

```rust
fn use_ref(r: &mut i32) {
    *r += 1;
}

fn main() {
    let mut x = 0;
    let r = &mut x;

    // This does NOT move r. It reborrows: &mut *r
    use_ref(r);
    use_ref(r); // Still works because r was reborrowed, not moved

    // Explicit reborrow syntax:
    use_ref(&mut *r);
}
```

Reborrowing is why you can call multiple `&mut self` methods on the same value in sequence. The compiler inserts implicit reborrows. This fails when you try to store the reborrow alongside the original -- that would be two live `&mut` references.

### Higher-Ranked Trait Bounds (HRTB)

`for<'a>` means "for any lifetime `'a`". You need this when a closure or function must work with references of any lifetime, not a specific one:

```rust
// Without HRTB -- this pins 'a to a single lifetime chosen by the caller
fn apply<'a>(f: fn(&'a str) -> &'a str, s: &'a str) -> &'a str {
    f(s)
}

// With HRTB -- f works for ANY lifetime
fn apply_hrtb(f: for<'a> fn(&'a str) -> &'a str, s: &str) -> &str {
    f(s)
}

// Most common in closure bounds:
fn apply_closure<F>(f: F, s: &str) -> usize
where
    F: for<'a> Fn(&'a str) -> usize,
{
    f(s)
}
```

In practice, `Fn(&str) -> usize` already desugars to `for<'a> Fn(&'a str) -> usize` through lifetime elision. You need explicit `for<'a>` when:
- The lifetime appears in a position elision does not cover
- You are writing trait bounds that involve references in both input and output
- You are using function pointers as trait bounds

### Lifetime Elision Edge Cases

The three elision rules handle most cases, but they fail in specific situations:

```rust
// Rule 1: Each reference param gets its own lifetime
// Rule 2: If exactly one input lifetime, it's used for all outputs
// Rule 3: If &self or &mut self, self's lifetime is used for outputs

struct Config {
    name: String,
}

impl Config {
    // Elision gives this: fn name(&'a self) -> &'a str
    // That's correct -- the output borrows from self.
    fn name(&self) -> &str {
        &self.name
    }

    // But what if you want to return something NOT borrowed from self?
    // Elision assumes it borrows from self, which is wrong.
    fn static_label(&self) -> &'static str {
        "config-v2"
    }

    // Multiple reference params with no self: elision fails, must annotate
    // fn pick(a: &str, b: &str) -> &str  // ERROR: ambiguous
    fn pick<'a>(a: &'a str, _b: &str) -> &'a str {
        a
    }
}
```

### Lifetimes in Trait Objects

Trait objects have an implicit lifetime bound:

```rust
// Box<dyn Trait> is actually Box<dyn Trait + 'static> in most contexts
// &'a dyn Trait is actually &'a (dyn Trait + 'a)

trait Processor {
    fn process(&self, data: &str) -> String;
}

// This requires the trait object to live for 'static:
fn store_processor(p: Box<dyn Processor>) {
    // Box<dyn Processor + 'static> -- p owns no borrowed data
}

// This allows the trait object to borrow data:
fn use_processor<'a>(p: &'a dyn Processor) {
    // The object itself can contain references with lifetime 'a
}

// Explicit lifetime on the trait object:
struct Registry<'a> {
    handlers: Vec<Box<dyn Processor + 'a>>,
}
```

The default lifetime for `Box<dyn Trait>` is `'static`. The default for `&'a dyn Trait` is `'a`. These defaults occasionally surprise you when a struct contains `Box<dyn Trait>` but the trait object holds borrowed data.

### Closure Lifetimes

Closures capture variables, and the captured references carry lifetimes:

```rust
fn make_greeting(name: &str) -> impl Fn() -> String + '_ {
    // Closure captures `name` by reference.
    // The '_ ties the return type's lifetime to `name`.
    move || format!("hello, {name}")
}

// Common mistake: returning a closure that borrows a local
// fn bad() -> impl Fn() -> &str {
//     let s = String::from("hi");
//     || &s  // ERROR: s is dropped, closure would dangle
// }
```

### GAT Lifetime Patterns

Generic Associated Types (stabilized in Rust 1.65) enable associated types that are generic over lifetimes. The canonical use case is a lending iterator:

```rust
trait LendingIterator {
    type Item<'a> where Self: 'a;

    fn next(&mut self) -> Option<Self::Item<'_>>;
}

// Windows that borrow from the iterator's internal buffer
struct WindowIter<'data> {
    data: &'data [i32],
    pos: usize,
    size: usize,
}

impl<'data> LendingIterator for WindowIter<'data> {
    type Item<'a> = &'a [i32] where Self: 'a;

    fn next(&mut self) -> Option<Self::Item<'_>> {
        if self.pos + self.size > self.data.len() {
            return None;
        }
        let window = &self.data[self.pos..self.pos + self.size];
        self.pos += 1;
        Some(window)
    }
}
```

Without GATs, you cannot express "the item borrows from the iterator itself" in a trait.

## Exercises

### Exercise 1: Variance Bug Hunt

The following code has three functions. Two compile, one does not. Predict which one fails, explain why in terms of variance, then fix it.

```rust
use std::cell::Cell;

fn covariant(input: &'static str) {
    let local = String::from("short-lived");
    let r: &str = &local; // shrink 'static to local -- covariant, fine
    println!("{r}");
    println!("{input}");
}

fn invariant_attempt(cell: &Cell<&'static str>) {
    let local = String::from("short-lived");
    cell.set(&local); // Would this work?
}

fn contravariant_fn(f: fn(&'static str)) {
    let local = String::from("short-lived");
    f(&local); // Does this work?
}
```

Write a test for each case. For the failing case, explain the soundness hole that variance prevents, and provide a corrected version.

**Hints:**
- `Cell<&'a T>` is invariant in `'a` -- you cannot widen or shrink the lifetime
- `fn(&'static str)` is contravariant in the argument: a function accepting `&'static str` can be called with any `&str` (right? or is it the other way around?)
- Think about what happens if `invariant_attempt` were allowed: who reads from the cell after `local` is dropped?

**Cargo.toml:**
```toml
[package]
name = "advanced-lifetimes"
edition = "2021"
```

<details>
<summary>Solution</summary>

```rust
use std::cell::Cell;

// COMPILES: &'a T is covariant in 'a.
// We are not "shrinking" 'static here. `input` keeps its lifetime.
// The local borrow `r` just has a shorter lifetime -- that is fine.
fn covariant(input: &'static str) {
    let local = String::from("short-lived");
    let r: &str = &local;
    println!("{r}");
    println!("{input}");
}

// DOES NOT COMPILE: Cell<&'a str> is invariant in 'a.
// If this compiled, you could:
//   1. Cell contains &'static str
//   2. Set it to &local (short-lived)
//   3. Caller reads Cell after local is dropped -> dangling reference
//
// fn invariant_attempt(cell: &Cell<&'static str>) {
//     let local = String::from("short-lived");
//     cell.set(&local); // ERROR: `local` does not live long enough
// }

// Fixed: accept the correct lifetime
fn invariant_fixed<'a>(cell: &Cell<&'a str>, replacement: &'a str) {
    cell.set(replacement); // OK: replacement lives as long as the cell expects
}

// COMPILES (but is subtle): fn(&'static str) is contravariant in the param.
// A fn(&'static str) can only be passed &'static str.
// But the CALLER of contravariant_fn passes a fn(&'static str), meaning
// the function pointer promises to only need 'static. We call it with &local,
// which is shorter. This does NOT compile as written.
//
// Actually: fn(&'static str) means "I require a 'static reference."
// Passing &local (non-'static) violates that requirement.
// Contravariance goes the other direction:
// fn(&'short str) is a subtype of fn(&'static str).
// A function that accepts ANY &str can be used where fn(&'static str) is expected.

// Let's demonstrate the correct direction:
fn contravariant_demo() {
    fn accepts_any(s: &str) {
        println!("{s}");
    }

    // A fn(&str) (which is fn for any lifetime) can be used
    // where fn(&'static str) is expected:
    let f: fn(&'static str) = accepts_any; // OK: contravariance
    f("hello");

    // But not the reverse:
    // let g: fn(&str) = requires_static; // Would be unsound
}

fn main() {
    covariant("forever");

    let cell = Cell::new("initial");
    let replacement = String::from("updated");
    invariant_fixed(&cell, &replacement);
    println!("cell: {}", cell.get());

    contravariant_demo();
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn covariant_works() {
        covariant("test");
    }

    #[test]
    fn invariant_fixed_works() {
        let val = String::from("new");
        let cell = Cell::new("old");
        invariant_fixed(&cell, &val);
        assert_eq!(cell.get(), "new");
    }

    #[test]
    fn contravariance_direction() {
        fn any_str(s: &str) -> usize { s.len() }

        // Contravariance: fn(&str) -> fn(&'static str)
        let f: fn(&'static str) -> usize = any_str;
        assert_eq!(f("hello"), 5);
    }
}
```

**Key insight:** Variance is not an academic curiosity. It is the mechanism that prevents you from smuggling a short-lived reference into a container that expects a long-lived one. Every time the compiler rejects a lifetime, it is preventing a potential dangling reference.
</details>

### Exercise 2: HRTB Callback Registry

Build a `CallbackRegistry` that stores named callbacks. Each callback takes a `&str` and returns a `usize`. The registry must support:

- `register(name, callback)` -- stores any `Fn(&str) -> usize`
- `call(name, input)` -- looks up and invokes the callback
- `map_all(input)` -- calls every callback and returns results

The challenge: the callbacks must work for references of any lifetime (HRTB), and the registry stores them as trait objects.

**Hints:**
- `Box<dyn Fn(&str) -> usize>` already implies `for<'a> Fn(&'a str) -> usize`
- The subtlety: if you try `Box<dyn for<'a> Fn(&'a str) -> &'a str>`, the return type borrows from input -- this is where HRTB becomes explicit
- Start simple, then add a `transform` variant that returns `&str`

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

// --- Simple version: callbacks return owned data ---

struct CallbackRegistry {
    callbacks: HashMap<String, Box<dyn Fn(&str) -> usize>>,
}

impl CallbackRegistry {
    fn new() -> Self {
        Self { callbacks: HashMap::new() }
    }

    fn register<F>(&mut self, name: &str, f: F)
    where
        F: Fn(&str) -> usize + 'static,
    {
        self.callbacks.insert(name.to_string(), Box::new(f));
    }

    fn call(&self, name: &str, input: &str) -> Option<usize> {
        self.callbacks.get(name).map(|f| f(input))
    }

    fn map_all(&self, input: &str) -> Vec<(&str, usize)> {
        self.callbacks
            .iter()
            .map(|(name, f)| (name.as_str(), f(input)))
            .collect()
    }
}

// --- Advanced version: HRTB with borrowed returns ---
// This stores functions that borrow from their input.

struct TransformRegistry {
    // Explicit HRTB: the function works for any input lifetime
    // and returns a reference with that same lifetime.
    transforms: HashMap<String, Box<dyn for<'a> Fn(&'a str) -> &'a str>>,
}

impl TransformRegistry {
    fn new() -> Self {
        Self { transforms: HashMap::new() }
    }

    fn register<F>(&mut self, name: &str, f: F)
    where
        F: for<'a> Fn(&'a str) -> &'a str + 'static,
    {
        self.transforms.insert(name.to_string(), Box::new(f));
    }

    fn apply(&self, name: &str, input: &str) -> Option<&str> {
        self.transforms.get(name).map(|f| f(input))
    }

    fn chain(&self, names: &[&str], input: &str) -> String {
        // Cannot chain borrowed returns (each borrows from previous),
        // so we must allocate intermediates.
        let mut current = input.to_string();
        for name in names {
            if let Some(f) = self.transforms.get(*name) {
                current = f(&current).to_string();
            }
        }
        current
    }
}

fn main() {
    let mut reg = CallbackRegistry::new();
    reg.register("len", |s| s.len());
    reg.register("words", |s| s.split_whitespace().count());

    println!("len: {:?}", reg.call("len", "hello world"));
    println!("all: {:?}", reg.map_all("hello world"));

    let mut transforms = TransformRegistry::new();
    transforms.register("trim", |s| s.trim());
    transforms.register("first_word", |s| {
        s.split_whitespace().next().unwrap_or("")
    });

    println!("trim: {:?}", transforms.apply("trim", "  hello  "));
    println!("chain: {}", transforms.chain(&["trim", "first_word"], "  hello world  "));
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn callback_registry() {
        let mut reg = CallbackRegistry::new();
        reg.register("len", |s| s.len());
        assert_eq!(reg.call("len", "hi"), Some(2));
        assert_eq!(reg.call("missing", "hi"), None);
    }

    #[test]
    fn transform_registry() {
        let mut reg = TransformRegistry::new();
        reg.register("trim", |s| s.trim());
        assert_eq!(reg.apply("trim", "  hi  "), Some("hi"));
    }

    #[test]
    fn hrtb_works_with_any_lifetime() {
        let mut reg = TransformRegistry::new();
        reg.register("identity", |s| s);

        // Works with a temporary:
        let result = reg.apply("identity", &String::from("temp")).unwrap();
        assert_eq!(result, "temp");

        // Works with a static:
        let result = reg.apply("identity", "static").unwrap();
        assert_eq!(result, "static");
    }
}
```

**Trade-off:** `TransformRegistry` callbacks cannot return data they allocate internally (the return borrows from the input only). If you need callbacks that return arbitrary borrowed data, you would need GATs or owned returns. The HRTB constraint `for<'a> Fn(&'a str) -> &'a str` precisely encodes "the output borrows from the input."
</details>

### Exercise 3: Lending Iterator with GATs

Implement a `LendingIterator` trait with a GAT `Item<'a>`. Then implement a `WindowsMut` iterator that yields overlapping mutable windows of a slice. This is impossible with `std::iter::Iterator` because the yielded mutable slice borrows from the iterator.

The iterator should:
- Hold a `&mut [T]` and a window size
- Yield `&mut [T]` slices that overlap by `window_size - 1`
- Each call to `next` advances by 1

**Hints:**
- The key constraint: `type Item<'a> where Self: 'a` -- the item can borrow from the iterator
- You will need unsafe to split the mutable borrow (or use `split_at_mut`)
- Compare this to `std::slice::Windows` which only yields `&[T]` (immutable) -- mutable overlapping windows are fundamentally impossible with `Iterator`

<details>
<summary>Solution</summary>

```rust
trait LendingIterator {
    type Item<'a> where Self: 'a;

    fn next(&mut self) -> Option<Self::Item<'_>>;
}

struct WindowsMut<'data, T> {
    // We store the full slice and an index.
    // Each call to next() reborrows the slice.
    data: &'data mut [T],
    pos: usize,
    window_size: usize,
}

impl<'data, T> WindowsMut<'data, T> {
    fn new(data: &'data mut [T], window_size: usize) -> Self {
        assert!(window_size > 0, "window size must be > 0");
        Self { data, pos: 0, window_size }
    }
}

impl<'data, T> LendingIterator for WindowsMut<'data, T> {
    type Item<'a> = &'a mut [T] where Self: 'a;

    fn next(&mut self) -> Option<Self::Item<'_>> {
        let end = self.pos + self.window_size;
        if end > self.data.len() {
            return None;
        }
        let window = &mut self.data[self.pos..end];
        self.pos += 1;
        Some(window)
    }
}

// A helper to consume all items from a lending iterator.
// Note: we cannot collect into a Vec because items borrow from the iterator.
fn count_items<I: LendingIterator>(mut iter: I) -> usize {
    let mut count = 0;
    while iter.next().is_some() {
        count += 1;
    }
    count
}

fn main() {
    let mut data = vec![1, 2, 3, 4, 5];
    let mut windows = WindowsMut::new(&mut data, 3);

    // Each window borrows from the iterator, so we cannot hold two at once.
    // This is exactly the constraint that makes mutable overlapping windows safe.
    if let Some(w) = windows.next() {
        println!("window 1: {w:?}");
        w[0] = 10; // Mutate through the window
    }
    if let Some(w) = windows.next() {
        println!("window 2: {w:?}"); // [2, 3, 4] -- element 0 was only in first window
    }
    if let Some(w) = windows.next() {
        println!("window 3: {w:?}");
    }
    assert!(windows.next().is_none());
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn windows_count() {
        let mut data = vec![1, 2, 3, 4, 5];
        let iter = WindowsMut::new(&mut data, 2);
        assert_eq!(count_items(iter), 4); // [1,2], [2,3], [3,4], [4,5]
    }

    #[test]
    fn mutation_visible() {
        let mut data = vec![1, 2, 3];
        let mut iter = WindowsMut::new(&mut data, 2);
        if let Some(w) = iter.next() {
            w[1] = 99;
        }
        // data[1] is now 99
        drop(iter);
        assert_eq!(data[1], 99);
    }

    #[test]
    fn window_size_equals_len() {
        let mut data = vec![1, 2, 3];
        let iter = WindowsMut::new(&mut data, 3);
        assert_eq!(count_items(iter), 1);
    }

    #[test]
    fn window_size_exceeds_len() {
        let mut data = vec![1, 2];
        let iter = WindowsMut::new(&mut data, 5);
        assert_eq!(count_items(iter), 0);
    }
}
```

**Why GATs matter:** `std::iter::Iterator` defines `type Item` without a lifetime parameter. The item cannot borrow from the iterator. GATs (`type Item<'a>`) lift this restriction. This enables lending iterators, streaming parsers, and zero-copy deserialization APIs that were previously impossible to express in generic Rust.

**Approach comparison:**

| Approach | Pros | Cons |
|---|---|---|
| GAT `LendingIterator` | Correct, safe, no unsafe needed | Cannot use `for` loops or standard iterator adapters |
| Unsafe raw pointer tricks | Works with `Iterator` trait | Unsound if not careful, hard to review |
| Collect into `Vec<&mut [T]>` | Simple | Impossible -- overlapping `&mut` violates aliasing |

</details>

## Common Mistakes

1. **Assuming `'static` means "lives forever."** `'static` means "can live as long as the program." Owned types like `String` are `'static`. It does not mean the value is immortal -- it means it contains no borrowed data that could dangle.

2. **Fighting the borrow checker with lifetimes.** If you need five lifetime parameters, your design is wrong. Restructure to reduce borrowing depth.

3. **Ignoring variance when using `Cell`/`RefCell`.** Interior mutability makes the container invariant. You cannot coerce `Cell<&'long T>` to `Cell<&'short T>`.

4. **Mixing up HRTB direction.** `for<'a> Fn(&'a str)` means the function works for any lifetime. This is a constraint on the function, not on the caller.

5. **Trying to store lending iterator items.** The entire point of a lending iterator is that each item borrows from the iterator. You cannot collect them without cloning.

## Verification

- All exercises should pass `cargo test`
- Exercise 1: try removing the `Cell` fix and confirm the compiler error matches your variance analysis
- Exercise 3: try implementing the same thing with `std::iter::Iterator` and observe why it fails

## Summary

Advanced lifetimes are the mechanism by which Rust enforces memory safety at the type level. Variance prevents smuggling references across lifetime boundaries. HRTB enables generic callbacks that work with any borrowed data. GATs finally close the expressiveness gap for lending patterns. Mastering these tools means you can design APIs that are both zero-cost and impossible to misuse.

## What's Next

Exercise 12 covers FFI and C interop -- where Rust's lifetime system meets the unmanaged world of C pointers and manual memory management.

## Resources

- [The Rustonomicon: Lifetimes](https://doc.rust-lang.org/nomicon/lifetimes.html)
- [The Rustonomicon: Subtyping and Variance](https://doc.rust-lang.org/nomicon/subtyping.html)
- [RFC 1598: GATs](https://rust-lang.github.io/rfcs/1598-generic_associated_types.html)
- [Common Rust Lifetime Misconceptions](https://github.com/pretzelhammer/rust-blog/blob/master/posts/common-rust-lifetime-misconceptions.md)
- [Crust of Rust: Lifetime Annotations (Jon Gjengset)](https://www.youtube.com/watch?v=rAl-9HwD858)
