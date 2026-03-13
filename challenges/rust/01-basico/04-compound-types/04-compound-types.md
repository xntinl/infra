# 4. Compound Types

**Difficulty**: Basico

## Prerequisites

- Completed exercises 1-3 (Hello Rust, Variables, Scalar Types)
- Understanding of scalar types (`i32`, `f64`, `bool`, `char`)
- Familiarity with `let` bindings and type annotations

## Learning Objectives

After completing this exercise, you will be able to:

- Create tuples with mixed types and access elements by destructuring and dot notation
- Create fixed-size arrays with uniform types and access elements by index
- Explain why out-of-bounds array access panics at runtime
- Use the array initialization syntax `[value; count]`
- Distinguish arrays from `Vec` and explain when to use each

## Concepts

### Why Compound Types Exist

Scalar types hold a single value. But programs constantly work with groups of related values — a coordinate is two numbers, a color is three, a row of data might be five fields. **Compound types** group multiple values into one type. Rust has two built-in compound types: **tuples** and **arrays**.

Both live on the **stack** (not the heap) and have a fixed size known at compile time. This makes them fast to allocate and access — the compiler knows exactly how many bytes they occupy.

### Tuples

A **tuple** groups a fixed number of values with potentially **different types**. Once declared, a tuple's length cannot change.

```rust
let point: (f64, f64) = (3.0, 4.0);
let mixed: (i32, f64, bool) = (42, 3.14, true);
```

Think of a tuple as an anonymous struct — it holds multiple fields, but the fields have positions (0, 1, 2) instead of names.

You access tuple elements in two ways:

**Destructuring** — bind each element to a variable:

```rust
let (x, y) = point;
```

**Dot notation** — access by index:

```rust
let first = point.0;
let second = point.1;
```

Note the syntax: `point.0`, not `point[0]`. Tuple indexing uses a dot followed by a literal number. You cannot use a variable as the index — the index must be known at compile time.

The **unit type** `()` is a special tuple with zero elements. It represents "no value" and is Rust's equivalent of `void` in C. Functions that do not return anything implicitly return `()`.

### Arrays

An **array** is a fixed-length collection of values that all have the **same type**. Arrays live on the stack.

```rust
let numbers: [i32; 5] = [1, 2, 3, 4, 5];
```

The type annotation `[i32; 5]` reads as "an array of five `i32` values." The length is part of the type — `[i32; 5]` and `[i32; 3]` are different types.

You access array elements with bracket indexing:

```rust
let first = numbers[0];
let third = numbers[2];
```

Unlike tuples, you can use a variable as the array index. But the index must be within bounds.

### Out-of-Bounds Access

If you try to access an array beyond its length, Rust panics at runtime:

```rust
let numbers = [1, 2, 3];
let bad = numbers[99]; // panic!
```

In C, this would be undefined behavior — you might read garbage memory, corrupt data, or open a security vulnerability. Rust guarantees safety: it checks every index at runtime and crashes immediately with a clear error rather than silently corrupting memory. This is called **bounds checking**.

### Array Initialization with `[value; count]`

To create an array where every element has the same value:

```rust
let zeros = [0; 10]; // [0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
```

The syntax is `[initial_value; length]`. This is useful for buffers, counters, and lookup tables.

### Arrays vs Vec

Arrays have a fixed size known at compile time. When you need a dynamic-size collection that can grow and shrink, use `Vec<T>` (a vector). `Vec` allocates on the heap and can change length:

```rust
let mut dynamic = vec![1, 2, 3];
dynamic.push(4); // now [1, 2, 3, 4]
```

| | Array `[T; N]` | Vec `Vec<T>` |
|---|---|---|
| Size | Fixed at compile time | Dynamic |
| Memory | Stack | Heap |
| Resizable | No | Yes (`push`, `pop`) |
| Performance | Very fast (no allocation) | Fast (amortized) |
| Use case | Fixed-size data, buffers | Collections that grow |

Use arrays when you know the exact size and it will not change. Use `Vec` when the size is determined at runtime or needs to change. We will cover `Vec` in depth in a later exercise.

## Exercises

### Exercise 1: Tuple Basics

Create a new project:

```
$ cargo new compound-types-lab
$ cd compound-types-lab
```

Edit `src/main.rs`:

```rust
fn main() {
    // Tuple with mixed types
    let server_config: (&str, u16, bool) = ("localhost", 8080, true);

    // Dot notation access
    let host = server_config.0;
    let port = server_config.1;
    let tls_enabled = server_config.2;

    println!("Host: {}", host);
    println!("Port: {}", port);
    println!("TLS: {}", tls_enabled);
    println!("Full config: {:?}", server_config);
}
```

**What's happening here:**

1. `("localhost", 8080, true)` creates a tuple with three elements of types `&str`, `u16`, and `bool`.
2. `.0`, `.1`, `.2` access elements by position. Positions start at zero.
3. `{:?}` is the **debug format** — it prints the value using its `Debug` implementation. Tuples implement `Debug` automatically, so you can print them without writing any extra code. The standard `{}` format does not work on tuples.

What do you think this will print?

```
$ cargo run
Host: localhost
Port: 8080
TLS: true
Full config: ("localhost", 8080, true)
```

### Exercise 2: Tuple Destructuring

Edit `src/main.rs`:

```rust
fn main() {
    let coordinates: (f64, f64, f64) = (1.0, 2.5, -3.7);

    // Destructure into individual variables
    let (x, y, z) = coordinates;
    println!("x: {}, y: {}, z: {}", x, y, z);

    // Ignore elements with underscore
    let (latitude, longitude, _) = coordinates;
    println!("2D position: ({}, {})", latitude, longitude);

    // Nested tuples
    let nested: ((i32, i32), (i32, i32)) = ((0, 0), (10, 20));
    let (start, end) = nested;
    println!("Start: {:?}", start);
    println!("End: {:?}", end);

    // Deep destructure
    let ((x1, y1), (x2, y2)) = nested;
    println!("From ({}, {}) to ({}, {})", x1, y1, x2, y2);

    // Unit type: the empty tuple
    let empty: () = ();
    println!("Unit type: {:?}", empty);
    println!("Unit size: {} bytes", std::mem::size_of::<()>());
}
```

**What's happening here:**

1. `let (x, y, z) = coordinates` is **destructuring** — it binds each element of the tuple to a separate variable. The number of variables must match the tuple length exactly.
2. `_` (underscore) means "ignore this element." The compiler does not create a binding for it and will not warn about an unused variable.
3. Tuples can be nested. You can destructure at any depth.
4. The unit type `()` has zero size. It exists conceptually but occupies no memory.

What do you think this will print?

```
$ cargo run
x: 1, y: 2.5, z: -3.7
2D position: (1, 2.5)
Start: (0, 0)
End: (10, 20)
From (0, 0) to (10, 20)
Unit type: ()
Unit size: 0 bytes
```

### Exercise 3: Array Basics

Edit `src/main.rs`:

```rust
fn main() {
    // Array: fixed size, same type
    let days: [&str; 7] = [
        "Monday",
        "Tuesday",
        "Wednesday",
        "Thursday",
        "Friday",
        "Saturday",
        "Sunday",
    ];

    println!("First day: {}", days[0]);
    println!("Last day: {}", days[6]);
    println!("Number of days: {}", days.len());
    println!("All days: {:?}", days);

    // Type is inferred when possible
    let primes = [2, 3, 5, 7, 11];
    println!("\nFirst 5 primes: {:?}", primes);
    println!("Array size: {} bytes", std::mem::size_of_val(&primes));

    // Initialize with repeated value
    let zeros = [0i32; 5];
    let filled = [true; 3];
    println!("\nZeros: {:?}", zeros);
    println!("Filled: {:?}", filled);

    // Iterate over an array
    print!("\nPrimes doubled:");
    for prime in primes {
        print!(" {}", prime * 2);
    }
    println!();
}
```

**What's happening here:**

1. `[&str; 7]` is an array of 7 string slices. The semicolon separates the element type from the count.
2. `days[0]` accesses the first element (zero-indexed, like C and most languages).
3. `days.len()` returns the array length as `usize`.
4. `[0i32; 5]` creates `[0, 0, 0, 0, 0]` with type `[i32; 5]`. The `i32` suffix clarifies the type of the initial value.
5. `for prime in primes` iterates over each element. Rust arrays implement the `IntoIterator` trait, so they work with `for` loops directly.

What do you think this will print?

```
$ cargo run
First day: Monday
Last day: Sunday
Number of days: 7
All days: ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"]

First 5 primes: [2, 3, 5, 7, 11]
Array size: 20 bytes

Zeros: [0, 0, 0, 0, 0]
Filled: [true, true, true]

Primes doubled: 4 6 10 14 22
```

The array of 5 `i32` values occupies 20 bytes: 5 elements times 4 bytes each. No overhead, no header — just raw data on the stack.

### Exercise 4: Out-of-Bounds Access

Edit `src/main.rs`:

```rust
fn main() {
    let values = [10, 20, 30, 40, 50];

    // Safe access with .get() — returns Option
    let third = values.get(2);
    let out_of_bounds = values.get(99);

    println!("values.get(2): {:?}", third);
    println!("values.get(99): {:?}", out_of_bounds);

    // Pattern matching on the result
    match values.get(2) {
        Some(value) => println!("Found: {}", value),
        None => println!("Index out of bounds"),
    }

    match values.get(99) {
        Some(value) => println!("Found: {}", value),
        None => println!("Index out of bounds"),
    }

    // Slicing: borrow a portion of the array
    let first_three = &values[0..3];
    let last_two = &values[3..];
    println!("\nFirst three: {:?}", first_three);
    println!("Last two: {:?}", last_two);
    println!("Full slice: {:?}", &values[..]);
}
```

**What's happening here:**

1. `.get(index)` returns `Option<&T>` — either `Some(&value)` if the index is valid, or `None` if it is out of bounds. Unlike `values[index]`, it never panics.
2. `match` is Rust's pattern matching — like a `switch` statement in C but much more powerful. We will cover it in depth later. For now, read it as "if `Some`, do this; if `None`, do that."
3. `&values[0..3]` creates a **slice** — a reference to a contiguous portion of the array. `0..3` means indices 0, 1, 2 (the end is exclusive). `3..` means "from index 3 to the end."

What do you think this will print?

```
$ cargo run
values.get(2): Some(30)
values.get(99): None
Found: 30
Index out of bounds

First three: [10, 20, 30]
Last two: [40, 50]
Full slice: [10, 20, 30, 40, 50]
```

To see what a panic looks like, temporarily replace the `get` calls with direct indexing. Add this line at the end of `main`:

```rust
    let _crash = values[99]; // this will panic
```

```
$ cargo run
thread 'main' panicked at 'index out of bounds: the len is 5 but the index is 99', src/main.rs:X:Y
```

The program immediately terminates with a clear message telling you the array length and the invalid index. Remove this line before continuing.

### Exercise 5: Putting It Together

Edit `src/main.rs`:

```rust
fn main() {
    // Student records: (name, grades_array)
    let student_a: (&str, [u32; 4]) = ("Alice", [85, 92, 78, 95]);
    let student_b: (&str, [u32; 4]) = ("Bob", [70, 65, 80, 72]);
    let student_c: (&str, [u32; 4]) = ("Carol", [90, 88, 94, 91]);

    // Store students in an array of tuples
    let students = [student_a, student_b, student_c];

    println!("--- Grade Report ---\n");

    for student in students {
        let (name, grades) = student;
        let mut total: u32 = 0;

        for grade in grades {
            total += grade;
        }

        let average = total as f64 / grades.len() as f64;
        let highest = find_max(grades);

        println!("Student: {}", name);
        println!("  Grades: {:?}", grades);
        println!("  Total: {}", total);
        println!("  Average: {:.1}", average);
        println!("  Highest: {}", highest);
        println!();
    }
}

fn find_max(values: [u32; 4]) -> u32 {
    let mut max = values[0];
    for value in values {
        if value > max {
            max = value;
        }
    }
    max
}
```

**What's happening here:**

1. Each student is a tuple of `(&str, [u32; 4])` — a name and an array of 4 grades.
2. `students` is an array of 3 tuples. Its type is `[(&str, [u32; 4]); 3]` — types nest cleanly.
3. The `for` loop destructures each student tuple into `name` and `grades`.
4. `total as f64 / grades.len() as f64` casts both sides to `f64` before division to get a float result. Without the casts, integer division would truncate.
5. `find_max` is a function that takes an array of exactly 4 `u32` values and returns the maximum. The `[u32; 4]` parameter type means this function only works with arrays of length 4 — the length is part of the type. We will learn how to write functions that accept arrays of any length when we cover slices.
6. `{:.1}` formats the float with 1 decimal place.

What do you think this will print?

```
$ cargo run
--- Grade Report ---

Student: Alice
  Grades: [85, 92, 78, 95]
  Total: 350
  Average: 87.5
  Highest: 95

Student: Bob
  Grades: [70, 65, 80, 72]
  Total: 287
  Average: 71.8
  Highest: 80

Student: Carol
  Grades: [90, 88, 94, 91]
  Total: 363
  Average: 90.8
  Highest: 94

```

## Common Mistakes

### Wrong Number of Variables in Destructuring

```rust
fn main() {
    let point = (1.0, 2.0, 3.0);
    let (x, y) = point;
}
```

```
error[E0308]: mismatched types
 --> src/main.rs:3:9
  |
3 |     let (x, y) = point;
  |         ^^^^^^ expected a tuple with 3 elements, found one with 2 elements
  |
  = note: expected tuple `(f64, f64, f64)`
             found tuple `(_, _)`
```

**Why:** The number of variables must match the tuple length exactly.
**Fix:** `let (x, y, _) = point;` — use `_` to ignore elements you do not need.

### Trying to Index a Tuple with a Variable

```rust
fn main() {
    let t = (10, 20, 30);
    let i = 1;
    let val = t.i; // does not compile
}
```

```
error[E0609]: no field `i` on type `({integer}, {integer}, {integer})`
```

**Why:** Tuple indices must be literal numbers (`t.0`, `t.1`), not variables. The compiler needs to know the index at compile time to determine the return type (each position can have a different type).
**Fix:** Use an array if you need variable indexing: `let a = [10, 20, 30]; let val = a[i];`

### Mismatched Array Lengths

```rust
fn main() {
    let a: [i32; 3] = [1, 2, 3, 4];
}
```

```
error[E0308]: mismatched types
 --> src/main.rs:2:24
  |
2 |     let a: [i32; 3] = [1, 2, 3, 4];
  |            --------   ^^^^^^^^^^^^ expected an array with a fixed size of 3 elements, found one with 4 elements
```

**Why:** The type says 3 elements but you provided 4. The length in the type annotation must match the actual number of elements.
**Fix:** Either change the annotation to `[i32; 4]` or remove an element.

### Mixing Types in an Array

```rust
fn main() {
    let mixed = [1, 2.0, true];
}
```

```
error[E0308]: mismatched types
 --> src/main.rs:2:22
  |
2 |     let mixed = [1, 2.0, true];
  |                     ^^^ expected integer, found floating-point number
```

**Why:** All elements in an array must have the same type. The compiler inferred `i32` from the first element and then found `f64` and `bool`.
**Fix:** Use a tuple for mixed types: `let mixed = (1, 2.0, true);`

## Verification

Run these commands from your project directory:

```
$ cargo check
    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.00s
```

```
$ cargo run
--- Grade Report ---

Student: Alice
  Grades: [85, 92, 78, 95]
  Total: 350
  Average: 87.5
  Highest: 95

Student: Bob
  Grades: [70, 65, 80, 72]
  Total: 287
  Average: 71.8
  Highest: 80

Student: Carol
  Grades: [90, 88, 94, 91]
  Total: 363
  Average: 90.8
  Highest: 94
```

If both commands succeed with no warnings and the output matches, you have completed this exercise.

## Summary

- **Key concepts**: Tuples (mixed types, fixed size, dot access), arrays (same type, fixed size, bracket access), destructuring, the unit type `()`, slices (`&[T]`), `Option` from `.get()`, `[value; count]` initialization
- **What you practiced**: Creating and accessing tuples, destructuring with `_`, creating and iterating arrays, safe access with `.get()`, slicing arrays, combining tuples and arrays, writing functions that accept arrays
- **Important to remember**: Tuples use dot notation (`t.0`), arrays use brackets (`a[0]`). Array length is part of the type. Out-of-bounds access panics — use `.get()` for safe access. When you need a growable collection, use `Vec` instead of arrays.

## What's Next

We used a `find_max` function in Exercise 5 but did not explain function mechanics in detail. In the next exercise, we will dive into functions, return values, and one of Rust's most important distinctions: **expressions vs statements**.

## Resources

- [The Rust Programming Language — Chapter 3.2: Data Types (Compound Types)](https://doc.rust-lang.org/book/ch03-02-data-types.html#compound-types)
- [Rust by Example — Tuples](https://doc.rust-lang.org/rust-by-example/primitives/tuples.html)
- [Rust by Example — Arrays and Slices](https://doc.rust-lang.org/rust-by-example/primitives/array.html)
