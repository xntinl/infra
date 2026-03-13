# 2. Generics

**Difficulty**: Intermedio

## Prerequisites
- Completed: 01-basico exercises (structs, enums, functions)
- Completed: 02-intermedio/01-traits (trait bounds, impl Trait)

## Learning Objectives
After completing this exercise, you will be able to:
- Define generic functions, structs, and enums
- Apply trait bounds to constrain generic type parameters
- Use turbofish syntax to disambiguate types
- Explain monomorphization and its performance implications
- Compare generics (static dispatch) with trait objects (dynamic dispatch)

## Concepts

### Why Generics?

Without generics, you'd write separate functions for every type:

```rust
fn largest_i32(list: &[i32]) -> &i32 { /* ... */ }
fn largest_f64(list: &[f64]) -> &f64 { /* ... */ }
fn largest_char(list: &[char]) -> &char { /* ... */ }
```

That's three copies of the same logic. Generics let you write it once:

```rust
fn largest<T: PartialOrd>(list: &[T]) -> &T {
    let mut largest = &list[0];
    for item in &list[1..] {
        if item > largest {
            largest = item;
        }
    }
    largest
}
```

The `<T: PartialOrd>` is a trait bound — it says "T can be any type, as long as it supports comparison." The compiler checks this at compile time.

### Monomorphization: Zero-Cost Abstraction

When you write `largest::<i32>` and `largest::<f64>`, the compiler generates two specialized functions — one for `i32`, one for `f64`. This is called monomorphization. You pay zero runtime cost for generics; the compiler does the work for you.

This is fundamentally different from how Java or Go handle generics. In Rust, generic code runs exactly as fast as hand-written specialized code.

### Generic Structs and Enums

You already know two generic enums from the standard library:

```rust
enum Option<T> {
    Some(T),
    None,
}

enum Result<T, E> {
    Ok(T),
    Err(E),
}
```

You can define your own:

```rust
struct Pair<T> {
    first: T,
    second: T,
}

struct KeyValue<K, V> {
    key: K,
    value: V,
}
```

### Implementing Methods on Generic Types

```rust
impl<T> Pair<T> {
    fn new(first: T, second: T) -> Self {
        Pair { first, second }
    }
}

// This method only exists for Pairs where T implements Display + PartialOrd
impl<T: std::fmt::Display + PartialOrd> Pair<T> {
    fn larger_display(&self) -> &T {
        if self.first >= self.second { &self.first } else { &self.second }
    }
}
```

That second `impl` block is conditional — `larger_display` only exists when `T` is both displayable and comparable. Try calling it on a `Pair<Vec<u8>>` and the compiler will refuse.

### Turbofish Syntax

Sometimes the compiler can't infer a generic type. You help it with turbofish `::<>`:

```rust
let numbers = vec![1, 2, 3, 4, 5];
let doubled: Vec<i32> = numbers.iter().map(|x| x * 2).collect();

// Or, equivalently, with turbofish:
let doubled = numbers.iter().map(|x| x * 2).collect::<Vec<i32>>();
```

Both are common. Turbofish is useful in chains where adding a type annotation on `let` would be awkward.

## Exercises

### Exercise 1: A Generic Function

```rust
// TODO: Write a generic function `first` that takes a slice &[T]
// and returns Option<&T> — the first element, or None if empty.
// No trait bounds needed here.

fn main() {
    let numbers = vec![10, 20, 30];
    let words = vec!["hello", "world"];
    let empty: Vec<i32> = vec![];

    println!("{:?}", first(&numbers));  // Some(10)
    println!("{:?}", first(&words));    // Some("hello")
    println!("{:?}", first(&empty));    // None
}
```

Now answer: why does this function need no trait bounds? What operation are we performing on `T`?

### Exercise 2: A Generic Struct

```rust
#[derive(Debug)]
struct Stack<T> {
    elements: Vec<T>,
}

// TODO: Implement these methods for Stack<T>:
//   fn new() -> Self
//   fn push(&mut self, item: T)
//   fn pop(&mut self) -> Option<T>
//   fn peek(&self) -> Option<&T>
//   fn is_empty(&self) -> bool
//   fn size(&self) -> usize

fn main() {
    let mut stack = Stack::new();
    stack.push(1);
    stack.push(2);
    stack.push(3);

    println!("size: {}", stack.size());       // 3
    println!("peek: {:?}", stack.peek());     // Some(3)
    println!("pop: {:?}", stack.pop());       // Some(3)
    println!("pop: {:?}", stack.pop());       // Some(2)
    println!("size: {}", stack.size());       // 1
    println!("empty? {}", stack.is_empty());  // false

    // It should also work with strings
    let mut names: Stack<String> = Stack::new();
    names.push(String::from("Alice"));
    names.push(String::from("Bob"));
    println!("top name: {:?}", names.peek()); // Some("Bob")
}
```

### Try It Yourself

Add a method `fn contains(&self, item: &T) -> bool` to `Stack<T>`, but only when `T: PartialEq`. Use a conditional impl block. Verify that `Stack<i32>` gets `contains` but think about what would happen with a type that doesn't implement `PartialEq`.

### Exercise 3: Multiple Type Parameters

```rust
#[derive(Debug)]
struct Pair<A, B> {
    first: A,
    second: B,
}

impl<A, B> Pair<A, B> {
    fn new(first: A, second: B) -> Self {
        Pair { first, second }
    }

    // TODO: Implement `swap` that returns a new Pair<B, A>
    // with the fields reversed. This consumes self.
    // fn swap(self) -> Pair<B, A>
}

// TODO: Implement Display for Pair<A, B> where both A and B implement Display.
// Format: "(first, second)"
// You'll need a where clause.

fn main() {
    let p = Pair::new(42, "hello");
    println!("{}", p);  // (42, hello)

    let swapped = p.swap();
    println!("{}", swapped);  // (hello, 42)

    // Different types work too
    let p2 = Pair::new(3.14, vec![1, 2, 3]);
    println!("{:?}", p2);  // Debug works because we derived it
    // println!("{}", p2);  // This WON'T compile. Why?
}
```

Think about why `println!("{}", p2)` won't compile. What trait is `Vec<i32>` missing?

### Exercise 4: Trait Bounds in Practice

```rust
use std::fmt;

trait Describable {
    fn describe(&self) -> String;
}

#[derive(Debug, Clone)]
struct Product {
    name: String,
    price: f64,
}

impl Describable for Product {
    fn describe(&self) -> String {
        format!("{} (${:.2})", self.name, self.price)
    }
}

impl fmt::Display for Product {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{}", self.describe())
    }
}

// TODO: Write a function `print_descriptions` that takes a slice of items
// where each item implements Describable + Display.
// It should print each item's description with a numbered prefix:
// "1. {description}"
// "2. {description}"
// Use a where clause.

// TODO: Write a function `find_by_description` that takes a slice of items
// implementing Describable and a search term (&str).
// Return Option<&T> — the first item whose describe() contains the search term.
// Hint: use .contains() on String.

fn main() {
    let products = vec![
        Product { name: String::from("Laptop"), price: 999.99 },
        Product { name: String::from("Mouse"), price: 29.50 },
        Product { name: String::from("Keyboard"), price: 79.00 },
    ];

    print_descriptions(&products);
    println!();

    match find_by_description(&products, "Mouse") {
        Some(p) => println!("Found: {}", p.describe()),
        None => println!("Not found"),
    }
}
```

### Exercise 5: Generics vs Trait Objects

This exercise doesn't require you to write code — it asks you to read, predict, and verify.

```rust
use std::fmt;

trait Shape: fmt::Display {
    fn area(&self) -> f64;
}

struct Circle { radius: f64 }
struct Rectangle { width: f64, height: f64 }

impl Shape for Circle {
    fn area(&self) -> f64 { std::f64::consts::PI * self.radius * self.radius }
}
impl fmt::Display for Circle {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "Circle(r={})", self.radius)
    }
}

impl Shape for Rectangle {
    fn area(&self) -> f64 { self.width * self.height }
}
impl fmt::Display for Rectangle {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "Rect({}x{})", self.width, self.height)
    }
}

// Static dispatch — monomorphized, one version per type
fn print_area_static(shape: &impl Shape) {
    println!("{}: area = {:.2}", shape, shape.area());
}

// Dynamic dispatch — one version, uses vtable at runtime
fn print_area_dynamic(shape: &dyn Shape) {
    println!("{}: area = {:.2}", shape, shape.area());
}

fn main() {
    let c = Circle { radius: 5.0 };
    let r = Rectangle { width: 4.0, height: 6.0 };

    // Both work identically from the caller's perspective
    print_area_static(&c);
    print_area_static(&r);

    print_area_dynamic(&c);
    print_area_dynamic(&r);

    // But only dynamic dispatch lets you do this:
    let shapes: Vec<&dyn Shape> = vec![&c, &r];
    for s in &shapes {
        println!("{}: {:.2}", s, s.area());
    }

    // TODO: Try creating a Vec<&impl Shape> — what happens? Why?
    // Think about what monomorphization means for collections of mixed types.
}
```

Answer these questions:
1. How many versions of `print_area_static` does the compiler generate?
2. How many versions of `print_area_dynamic` does the compiler generate?
3. Why can't you have a `Vec<&impl Shape>` with both circles and rectangles?

## Common Mistakes

### Mistake 1: Missing Trait Bounds

```rust
fn print_it<T>(item: T) {
    println!("{}", item); // ERROR
}
```

**Error**: `T doesn't implement std::fmt::Display`

The compiler doesn't know what `T` is. You must constrain it:

```rust
fn print_it<T: std::fmt::Display>(item: T) {
    println!("{}", item);
}
```

### Mistake 2: Turbofish in the Wrong Place

```rust
let x = "42".parse::<i32>().unwrap();   // Correct
let x = "42"::<i32>.parse().unwrap();   // Wrong — turbofish goes on the method
```

### Mistake 3: Expecting Generics to Work Like Interfaces

```rust
// You might expect this to work:
fn make_shape<T: Shape>() -> T {
    Circle { radius: 1.0 } // ERROR — T could be Rectangle!
}
```

The caller chooses `T`, not the function. If you want the function to choose the return type, use `impl Shape`:

```rust
fn make_shape() -> impl Shape {
    Circle { radius: 1.0 } // This is fine
}
```

## Verification

```bash
cargo run
```

For each exercise:
1. Verify the output matches expectations.
2. Try removing a trait bound — read the compiler error carefully. Does it tell you exactly which bound is missing?
3. In Exercise 5, try to store mixed types in a `Vec` using generics (not trait objects). Understand why it fails.

## Summary

Generics let you write code once for many types. Trait bounds constrain what those types can do. Monomorphization makes generics zero-cost — the compiler specializes each usage into concrete code. When you need mixed types in a collection, you need trait objects (`dyn Trait`), which use dynamic dispatch at a small runtime cost.

## What's Next

Exercise 03-lifetimes tackles the other kind of generic parameter — lifetime parameters that tell the compiler how long references are valid.

## Resources

- [The Rust Book — Generic Data Types](https://doc.rust-lang.org/book/ch10-01-syntax.html)
- [The Rust Book — Traits as Bounds](https://doc.rust-lang.org/book/ch10-02-traits.html#traits-as-parameters)
- [Rust Performance Book — Static vs Dynamic Dispatch](https://nnethercote.github.io/perf-book/type-sizes.html)
