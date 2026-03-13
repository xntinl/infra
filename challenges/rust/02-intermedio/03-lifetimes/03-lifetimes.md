# 3. Lifetimes

**Difficulty**: Intermedio

## Prerequisites
- Completed: 01-basico exercises (ownership, borrowing, references)
- Completed: 02-intermedio/01-traits, 02-intermedio/02-generics

## Learning Objectives
After completing this exercise, you will be able to:
- Explain why lifetime annotations exist and what problem they solve
- Annotate function signatures with lifetime parameters
- Apply the three lifetime elision rules to predict when annotations are unnecessary
- Use lifetimes in struct definitions to hold references safely
- Distinguish `'static` from other lifetimes

## Concepts

### Why Lifetimes Exist

You know from the basics that Rust prevents dangling references. The borrow checker enforces this. But sometimes the compiler can't figure out the relationships between references on its own — that's where lifetime annotations come in.

Lifetimes don't change how long values live. They describe relationships between references so the compiler can verify safety. Think of them as labels, not controls.

### The Problem

```rust
fn longest(x: &str, y: &str) -> &str {
    if x.len() > y.len() { x } else { y }
}
```

This won't compile. The compiler asks: "The return value is a reference, but is it tied to `x` or `y`?" It could be either, depending on runtime values. The compiler needs you to state the relationship explicitly.

### Lifetime Annotation Syntax

```rust
fn longest<'a>(x: &'a str, y: &'a str) -> &'a str {
    if x.len() > y.len() { x } else { y }
}
```

`'a` is a lifetime parameter. This signature says: "the returned reference will be valid for as long as both `x` and `y` are valid." In practice, the compiler assigns `'a` the shorter of the two input lifetimes.

### Lifetime Elision Rules

The compiler applies three rules to infer lifetimes so you don't always need to write them. If these rules fully determine all lifetimes, you don't need annotations:

1. **Each reference parameter gets its own lifetime.** `fn foo(x: &str, y: &str)` becomes `fn foo<'a, 'b>(x: &'a str, y: &'b str)`.

2. **If there's exactly one input lifetime, it's assigned to all output lifetimes.** `fn foo(x: &str) -> &str` becomes `fn foo<'a>(x: &'a str) -> &'a str`.

3. **If one parameter is `&self` or `&mut self`, the lifetime of self is assigned to all output lifetimes.**

When these rules aren't enough (like our `longest` example with two input lifetimes and one output lifetime), you must annotate manually.

### Lifetimes in Structs

A struct that holds a reference must declare the lifetime:

```rust
struct Excerpt<'a> {
    text: &'a str,
}
```

This says: "an `Excerpt` cannot outlive the string it references." The compiler enforces this everywhere the struct is used.

### The `'static` Lifetime

`'static` means "this reference is valid for the entire program duration." String literals have this lifetime:

```rust
let s: &'static str = "I live forever";
```

Most of the time, if the compiler suggests adding `'static`, something else is wrong. Don't reach for `'static` as a first fix.

## Exercises

### Exercise 1: Basic Lifetime Annotations

```rust
// This won't compile. Fix it by adding lifetime annotations.

fn longest(x: &str, y: &str) -> &str {
    if x.len() > y.len() {
        x
    } else {
        y
    }
}

fn main() {
    let result;
    let string1 = String::from("long string");

    {
        let string2 = String::from("xyz");
        result = longest(string1.as_str(), string2.as_str());
        println!("Longest: {}", result);
    }

    // TODO: Now try moving the println! AFTER the closing brace above.
    // Does it compile? Why or why not?
    // Think about which lifetime 'a gets bound to.
}
```

### Exercise 2: Predicting Elision

For each function signature, predict whether the compiler can infer lifetimes without annotations. Write "elision works" or "needs annotation" and explain which rule(s) apply:

```rust
// 1. One input reference, one output reference
fn first_word(s: &str) -> &str {
    s.split_whitespace().next().unwrap_or("")
}

// 2. Two input references, one output reference
fn pick_longer(a: &str, b: &str) -> &str {
    if a.len() >= b.len() { a } else { b }
}

// 3. Method with &self
struct Config {
    name: String,
}
impl Config {
    fn name(&self) -> &str {
        &self.name
    }
}

// 4. No references in return type
fn total_length(a: &str, b: &str) -> usize {
    a.len() + b.len()
}

// TODO: Copy each function into a file and test your predictions.
// For #2, you'll need to add the annotation. For the others, verify
// that they compile as-is.
```

### Try It Yourself

Write a function `first_line(text: &str) -> &str` that returns everything up to the first newline (or the entire string if there's no newline). Does it need an explicit lifetime annotation? Apply the elision rules to check.

### Exercise 3: Lifetimes in Structs

```rust
// TODO: Add the necessary lifetime annotations to make this compile.

struct Highlight {
    text: &str,
    start: usize,
    end: usize,
}

impl Highlight {
    fn new(text: &str, start: usize, end: usize) -> Self {
        Highlight { text, start, end }
    }

    fn highlighted(&self) -> &str {
        &self.text[self.start..self.end]
    }
}

fn main() {
    let document = String::from("Rust is fast and safe");
    let h = Highlight::new(&document, 0, 4);
    println!("Highlighted: '{}'", h.highlighted()); // "Rust"

    // TODO: Try this — move document creation inside a block:
    // let h;
    // {
    //     let document = String::from("Rust is fast and safe");
    //     h = Highlight::new(&document, 0, 4);
    // }
    // println!("{}", h.highlighted());
    //
    // Does it compile? Why not?
}
```

### Exercise 4: Multiple Lifetimes

Sometimes references in a function come from different sources with different lifetimes:

```rust
// TODO: Add lifetime annotations. Think carefully — does the return value
// relate to `text` or to `prefix`? Use different lifetime parameters
// if they have different relationships.

fn strip_prefix(text: &str, prefix: &str) -> &str {
    if text.starts_with(prefix) {
        &text[prefix.len()..]
    } else {
        text
    }
}

fn main() {
    let text = String::from("hello world");
    let result;
    {
        let prefix = String::from("hello ");
        result = strip_prefix(&text, &prefix);
    }
    // result only depends on `text`, not on `prefix`
    // So this should compile if lifetimes are annotated correctly.
    println!("{}", result);
}
```

Key insight: the return value only borrows from `text`, never from `prefix`. Your lifetime annotations should reflect this — `text` and the return value share a lifetime, but `prefix` gets its own.

### Exercise 5: Lifetime Bounds on Generics

```rust
use std::fmt;

// TODO: Fix the lifetime annotations on this struct and function.

struct Wrapper<T> {
    value: &T,
}

impl<T: fmt::Display> fmt::Display for Wrapper<T> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "Wrapped({})", self.value)
    }
}

fn wrap<T>(value: &T) -> Wrapper<T> {
    Wrapper { value }
}

fn longest_wrapped<T: fmt::Display>(a: &Wrapper<T>, b: &Wrapper<T>) -> &T {
    // Return the inner value of whichever wrapper's Display output is longer
    let a_str = format!("{}", a.value);
    let b_str = format!("{}", b.value);
    if a_str.len() >= b_str.len() {
        a.value
    } else {
        b.value
    }
}

fn main() {
    let x = 42;
    let y = 1000;

    let wx = wrap(&x);
    let wy = wrap(&y);

    println!("{}", wx);
    println!("{}", wy);

    let result = longest_wrapped(&wx, &wy);
    println!("Longest display: {}", result);
}
```

Hints:
- `Wrapper` holds a reference, so it needs a lifetime parameter.
- `wrap` returns a `Wrapper` that borrows from its input.
- `longest_wrapped` returns a reference — which inputs does it relate to?

## Common Mistakes

### Mistake 1: Returning a Reference to a Local Value

```rust
fn make_greeting(name: &str) -> &str {
    let greeting = format!("Hello, {}!", name);
    &greeting // ERROR — greeting is dropped at end of function
}
```

**Error**: `cannot return reference to local variable 'greeting'`

**Fix**: Return an owned `String` instead:

```rust
fn make_greeting(name: &str) -> String {
    format!("Hello, {}!", name)
}
```

Lifetime annotations can't extend how long a value lives. If you create a value inside a function, you must return it by ownership, not by reference.

### Mistake 2: Reaching for `'static` to Fix Everything

```rust
fn longest(x: &str, y: &str) -> &'static str {
    if x.len() > y.len() { x } else { y } // ERROR
}
```

`x` and `y` aren't `'static` — they're borrowed from somewhere. You can't promise the return value lives forever when it comes from a non-static source. Use a proper lifetime parameter instead.

### Mistake 3: Overly Restrictive Lifetimes

```rust
// Forces both inputs to have the same lifetime — unnecessarily restrictive
fn first_of_two<'a>(x: &'a str, _y: &'a str) -> &'a str {
    x // Only uses x, but y is constrained to 'a too
}
```

If the return only depends on `x`, give `y` its own lifetime:

```rust
fn first_of_two<'a, 'b>(x: &'a str, _y: &'b str) -> &'a str {
    x
}
```

This lets the caller pass references with different lifetimes.

## Verification

```bash
cargo run
```

For each exercise:
1. Start without annotations, read the compiler error.
2. Add annotations, verify it compiles.
3. Try the commented-out "dangerous" code blocks — verify the compiler catches the dangling reference.

The compiler errors for lifetime issues are among Rust's most informative. Read them carefully — they often tell you exactly what annotation is needed.

## Summary

Lifetime annotations describe relationships between references — they don't change how long values live. The three elision rules handle most cases automatically. When the compiler can't infer relationships (multiple input references, structs holding references), you add annotations. `'static` means "lives for the whole program" and is rarely the right fix. When in doubt, the compiler's error messages will guide you.

## What's Next

Exercise 04-closures introduces closures, which capture references from their environment — a direct application of what you've learned about borrowing and lifetimes.

## Resources

- [The Rust Book — Validating References with Lifetimes](https://doc.rust-lang.org/book/ch10-03-lifetime-syntax.html)
- [Rust by Example — Lifetimes](https://doc.rust-lang.org/rust-by-example/scope/lifetime.html)
- [Common Rust Lifetime Misconceptions](https://github.com/pretzelhammer/rust-blog/blob/master/posts/common-rust-lifetime-misconceptions.md)
