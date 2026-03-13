# 1. Traits

**Difficulty**: Intermedio

## Prerequisites
- Completed: 01-basico exercises (structs, enums, ownership, borrowing)
- Comfortable defining structs and implementing methods with `impl`

## Learning Objectives
After completing this exercise, you will be able to:
- Define traits and implement them for your own types
- Use default method implementations to reduce boilerplate
- Constrain generic functions with trait bounds
- Apply standard library traits (Display, Debug, Clone, PartialEq)
- Understand the orphan rule and why it exists

## Concepts

### What Are Traits?

A trait defines shared behavior. Think of it as a contract: any type that implements the trait guarantees it provides certain methods. This is how Rust achieves polymorphism without inheritance.

You already know how to add methods to a struct with `impl`. Traits let you say "these different types all share this capability."

```rust
trait Summary {
    fn summarize(&self) -> String;
}
```

Any type that implements `Summary` must provide a `summarize` method. The compiler enforces this at compile time — no runtime surprises.

### Default Implementations

Traits can provide default method bodies. Implementors can override them or use the default:

```rust
trait Summary {
    fn summarize_author(&self) -> String;

    fn summarize(&self) -> String {
        format!("(Read more from {}...)", self.summarize_author())
    }
}
```

Default methods can call other methods in the same trait, even ones without defaults. This lets you build rich interfaces where implementors only need to define a few core methods.

### Traits as Parameters and Trait Bounds

There are two equivalent syntaxes for accepting "anything that implements a trait":

```rust
// impl Trait syntax (concise, good for simple cases)
fn notify(item: &impl Summary) {
    println!("Breaking: {}", item.summarize());
}

// Trait bound syntax (flexible, needed for complex cases)
fn notify<T: Summary>(item: &T) {
    println!("Breaking: {}", item.summarize());
}
```

Multiple bounds use `+`:

```rust
fn display_summary(item: &(impl Summary + std::fmt::Display)) {
    println!("{}", item);
}
```

When bounds get long, use a `where` clause:

```rust
fn process<T, U>(t: &T, u: &U) -> String
where
    T: Summary + Clone,
    U: Summary + std::fmt::Debug,
{
    format!("{} and {:?}", t.summarize(), u)
}
```

### The Orphan Rule

You can implement a trait for a type only if either the trait or the type is local to your crate. You cannot implement `Display` for `Vec<T>` — both are defined in the standard library. This prevents conflicting implementations across crates.

### Supertraits

A trait can require that implementors also implement another trait:

```rust
trait PrettyPrint: std::fmt::Display {
    fn pretty_print(&self) {
        println!("=== {} ===", self); // can use Display because it's required
    }
}
```

## Exercises

### Exercise 1: Define and Implement a Trait

```rust
struct Article {
    title: String,
    author: String,
    content: String,
}

struct Tweet {
    username: String,
    body: String,
    reply: bool,
}

// TODO: Define a trait called `Summary` with one required method:
//   fn summarize(&self) -> String;

// TODO: Implement Summary for Article
// It should return: "{title} by {author}"

// TODO: Implement Summary for Tweet
// It should return: "@{username}: {body}"

fn main() {
    let article = Article {
        title: String::from("Rust 2026"),
        author: String::from("The Rust Team"),
        content: String::from("Great things are happening."),
    };

    let tweet = Tweet {
        username: String::from("rustlang"),
        body: String::from("Rust 2026 is here!"),
        reply: false,
    };

    println!("{}", article.summarize());
    println!("{}", tweet.summarize());
}
```

Expected output:
```
Rust 2026 by The Rust Team
@rustlang: Rust 2026 is here!
```

### Exercise 2: Default Methods

Extend your `Summary` trait with a default method:

```rust
trait Summary {
    fn summarize_author(&self) -> String;

    // This default method calls summarize_author()
    fn summarize(&self) -> String {
        format!("(Read more from {}...)", self.summarize_author())
    }
}

struct Article {
    title: String,
    author: String,
}

struct Tweet {
    username: String,
    body: String,
}

// TODO: Implement Summary for Article — override summarize() to return
//   "{title} by {author}"
//   (You must still implement summarize_author)

// TODO: Implement Summary for Tweet — use the DEFAULT summarize()
//   Only implement summarize_author to return "@{username}"

fn main() {
    let article = Article {
        title: String::from("Ownership Deep Dive"),
        author: String::from("Alice"),
    };
    let tweet = Tweet {
        username: String::from("bob"),
        body: String::from("hello world"),
    };

    println!("{}", article.summarize());
    println!("{}", tweet.summarize());
}
```

Expected output:
```
Ownership Deep Dive by Alice
(Read more from @bob...)
```

### Try It Yourself

Add a `word_count(&self) -> usize` default method to `Summary` that returns 0. Override it in `Article` to return the actual word count of `content`. Verify that `Tweet` returns 0 (default) while `Article` returns a real count.

### Exercise 3: Trait Bounds and Where Clauses

```rust
use std::fmt;

trait Summary {
    fn summarize(&self) -> String;
}

#[derive(Debug)]
struct Article {
    title: String,
}

impl Summary for Article {
    fn summarize(&self) -> String {
        self.title.clone()
    }
}

impl fmt::Display for Article {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "Article: {}", self.title)
    }
}

// TODO: Write a function `notify` that takes any type implementing Summary
// and prints "Breaking news: {summarize()}"
// Use impl Trait syntax.

// TODO: Write a function `debug_notify` that takes any type implementing
// BOTH Summary and Debug, and prints the debug representation followed
// by the summary. Use where clause syntax.

fn main() {
    let a = Article {
        title: String::from("Traits are powerful"),
    };

    notify(&a);
    debug_notify(&a);
}
```

### Exercise 4: Standard Library Traits

```rust
use std::fmt;

struct Point {
    x: f64,
    y: f64,
}

// TODO: Implement Display for Point so it prints as "(x, y)"

// TODO: Implement PartialEq for Point so two points are equal
// if both x and y are equal

// TODO: Implement Clone for Point manually (not with derive)
// Hint: return Point { x: self.x, y: self.y }

fn main() {
    let p1 = Point { x: 1.0, y: 2.0 };
    let p2 = p1.clone();

    println!("p1 = {}", p1);
    println!("p2 = {}", p2);
    println!("equal? {}", p1 == p2);

    let p3 = Point { x: 3.0, y: 4.0 };
    println!("p1 == p3? {}", p1 == p3);
}
```

Expected output:
```
p1 = (1, 2)
p2 = (1, 2)
equal? true
p1 == p3? false
```

### Exercise 5: Supertraits and Returning Impl Trait

```rust
use std::fmt;

// This trait requires that any implementor also implements Display
trait Printable: fmt::Display {
    fn print(&self) {
        println!("[Printable] {}", self);
    }
}

struct Temperature {
    celsius: f64,
}

// TODO: Implement Display for Temperature, showing e.g. "23.5°C"

// TODO: Implement Printable for Temperature (the default print() is fine)

// TODO: Write a function `coldest` that takes two &impl Printable items
// and returns... wait. We can't return a reference to a parameter easily here.
// Instead, write a function `make_freezing` that returns impl Printable.
// It should return a Temperature with celsius = 0.0.

fn main() {
    let t = Temperature { celsius: 36.6 };
    t.print();

    let cold = make_freezing();
    cold.print();
    println!("Direct display: {}", cold);
}
```

## Common Mistakes

### Mistake 1: Forgetting the Orphan Rule

```rust
// This won't compile — both Vec and Display are external
impl std::fmt::Display for Vec<i32> {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(f, "my vec")
    }
}
```

**Error**: `only traits defined in the current crate can be implemented for types defined outside of the crate`

**Fix**: Wrap the external type in a newtype:

```rust
struct Wrapper(Vec<i32>);

impl std::fmt::Display for Wrapper {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(f, "[{}]", self.0.iter()
            .map(|n| n.to_string())
            .collect::<Vec<_>>()
            .join(", "))
    }
}
```

### Mistake 2: Conflicting Default and Required Methods

```rust
trait Greet {
    fn name(&self) -> &str;
    fn greet(&self) { println!("Hello, {}!", self.name()); }
}

struct User { name: String }

impl Greet for User {
    // Forgot to implement name() — compiler error!
    // You must implement ALL required (non-default) methods.
}
```

**Error**: `not all trait items implemented, missing: 'name'`

**Fix**: Implement every method that has no default body.

## Verification

```bash
cargo run
```

For each exercise, check:
1. Does the output match what you expect?
2. Try removing a trait implementation — what error do you get?
3. Try implementing a trait you don't own for a type you don't own — verify you hit the orphan rule.

## Summary

Traits define shared behavior across types. Default methods reduce boilerplate. Trait bounds constrain generics at compile time. The orphan rule prevents implementation conflicts. Standard library traits like Display, Debug, Clone, and PartialEq give your types first-class behavior.

## What's Next

Exercise 02-generics builds directly on traits — you'll use trait bounds to constrain generic types and functions.

## Resources

- [The Rust Book — Traits](https://doc.rust-lang.org/book/ch10-02-traits.html)
- [Rust by Example — Traits](https://doc.rust-lang.org/rust-by-example/trait.html)
- [std::fmt::Display](https://doc.rust-lang.org/std/fmt/trait.Display.html)
