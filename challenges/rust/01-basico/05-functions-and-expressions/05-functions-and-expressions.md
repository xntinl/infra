# 5. Functions and Expressions

**Difficulty**: Basico

## Prerequisites

- Completed exercises 1-4 (Hello Rust, Variables, Scalar Types, Compound Types)
- Understanding of `let` bindings, basic types, tuples, and arrays

## Learning Objectives

After completing this exercise, you will be able to:

- Declare functions with typed parameters and return values
- Explain the difference between expressions and statements in Rust
- Use implicit returns (the last expression in a function body)
- Use explicit `return` for early exits
- Use blocks as expressions to compute values inline
- Predict whether adding or removing a semicolon changes the meaning of code

## Concepts

### Why Functions Work This Way in Rust

Every language has functions, but Rust makes one choice that changes how you write code: nearly everything is an **expression**. An expression evaluates to a value. A statement performs an action but does not produce a value. In Rust, blocks (`{}`), `if/else`, `match`, and most constructs are expressions — they return values. This eliminates a lot of temporary variables and makes code more concise.

If you come from C, Java, or Python, you are used to statements everywhere. In Rust, think more like Haskell or Ruby — the last expression in a block is the block's value.

### Function Declaration

Functions are declared with `fn`:

```rust
fn function_name(param1: Type1, param2: Type2) -> ReturnType {
    // body
}
```

Key rules:

- **All parameters must have type annotations.** Rust never infers parameter types. This is deliberate — function signatures serve as documentation and API contracts.
- **Return type is declared with `-> Type`.** If omitted, the function returns `()` (the unit type — equivalent to `void`).
- **Function names use `snake_case`** by convention (and compiler warning).
- **Functions can be declared in any order.** Unlike C, you do not need forward declarations. `main` can call a function defined below it.

### Expressions vs Statements

This is the single most important concept in this exercise.

A **statement** performs an action and returns nothing (`()`):

```rust
let x = 5;        // statement (let binding)
println!("hi");   // statement (expression + semicolon)
```

An **expression** evaluates to a value:

```rust
5                  // expression: evaluates to 5
x + 1              // expression: evaluates to sum
{                  // block expression:
    let a = 2;
    a + 3          // evaluates to 5
}
```

The critical rule: **a semicolon turns an expression into a statement.** Adding `;` after an expression discards its value and makes it return `()` instead.

```rust
let y = {
    let x = 3;
    x + 1       // no semicolon: this is the block's value (4)
};

let z = {
    let x = 3;
    x + 1;      // semicolon: expression becomes statement, block returns ()
};
```

In the first block, `y` is `4`. In the second, `z` is `()` — and the compiler will warn you or error if you expected an integer.

### Implicit Return

The last expression in a function body — without a semicolon — is the function's return value:

```rust
fn add(a: i32, b: i32) -> i32 {
    a + b   // no semicolon: this is the return value
}
```

This is not special syntax. The function body is a block expression, and the last expression in the block becomes the block's value, which becomes the function's return value. The mechanism is the same.

### Explicit return

Use the `return` keyword for early exits:

```rust
fn absolute_value(x: i32) -> i32 {
    if x < 0 {
        return -x;
    }
    x
}
```

`return` short-circuits the function. Use it when you need to exit before reaching the end. For the final value, the convention is to use the implicit return (no `return` keyword, no semicolon).

## Exercises

### Exercise 1: Basic Function Declaration

Create a new project:

```
$ cargo new functions-lab
$ cd functions-lab
```

Edit `src/main.rs`:

```rust
fn main() {
    greet("Rust");
    greet("developer");

    let result = add(3, 7);
    println!("3 + 7 = {}", result);

    let doubled = double(21);
    println!("double(21) = {}", doubled);
}

fn greet(name: &str) {
    println!("Hello, {}!", name);
}

fn add(a: i32, b: i32) -> i32 {
    a + b
}

fn double(x: i32) -> i32 {
    x * 2
}
```

**What's happening here:**

1. `fn greet(name: &str)` takes one parameter of type `&str` (a string slice). There is no `-> Type`, so it returns `()`.
2. `fn add(a: i32, b: i32) -> i32` takes two `i32` parameters and returns an `i32`. The body `a + b` has no semicolon — it is the return value.
3. Functions are defined after `main` but called from inside `main`. Rust resolves functions by name regardless of declaration order.

What do you think this will print?

```
$ cargo run
Hello, Rust!
Hello, developer!
3 + 7 = 10
double(21) = 42
```

### Exercise 2: Expressions vs Statements

Edit `src/main.rs`:

```rust
fn main() {
    // A block is an expression — it evaluates to its last expression
    let x = {
        let a = 5;
        let b = 10;
        a + b       // no semicolon: this is the block's value
    };
    println!("Block result: {}", x);

    // If/else is an expression too
    let temperature = 35;
    let category = if temperature > 30 {
        "hot"
    } else if temperature > 20 {
        "warm"
    } else {
        "cold"
    };
    println!("{}°C is {}", temperature, category);

    // Expressions can be nested
    let score = 85;
    let grade = {
        let normalized = score as f64 / 100.0;
        if normalized >= 0.9 {
            'A'
        } else if normalized >= 0.8 {
            'B'
        } else if normalized >= 0.7 {
            'C'
        } else {
            'F'
        }
    };
    println!("Score {} -> Grade {}", score, grade);

    // Semicolons discard the expression value
    let with_value = { 42 };        // block returns 42
    let unit_value = { 42; };       // semicolon discards value, block returns ()

    println!("with_value: {}", with_value);
    println!("unit_value: {:?}", unit_value);
}
```

**What's happening here:**

1. `let x = { ... }` assigns the value of a block expression to `x`. The block creates its own scope — `a` and `b` do not exist outside the braces.
2. `if/else` is an expression that returns a value. This works like the ternary operator (`?:`) in C, but it extends to any number of branches. All branches must return the same type.
3. `{ 42 }` evaluates to `42`. `{ 42; }` evaluates to `()` because the semicolon turns the expression into a statement.

What do you think this will print?

```
$ cargo run
Block result: 15
35°C is hot
Score 85 -> Grade B
with_value: 42
unit_value: ()
```

### Exercise 3: Implicit vs Explicit Return

Edit `src/main.rs`:

```rust
fn main() {
    println!("abs(-5) = {}", absolute_value(-5));
    println!("abs(3) = {}", absolute_value(3));
    println!("abs(0) = {}", absolute_value(0));

    println!("\nmax(10, 20) = {}", max_of_two(10, 20));
    println!("max(20, 10) = {}", max_of_two(20, 10));
    println!("max(5, 5) = {}", max_of_two(5, 5));

    println!("\nfirst_positive(&[-3, -1, 0, 4, 7]) = {:?}",
        first_positive(&[-3, -1, 0, 4, 7]));
    println!("first_positive(&[-3, -1, -5]) = {:?}",
        first_positive(&[-3, -1, -5]));
}

// Implicit return: last expression is the return value
fn absolute_value(x: i32) -> i32 {
    if x < 0 {
        -x
    } else {
        x
    }
}

// Also implicit return: the entire if/else is an expression
fn max_of_two(a: i32, b: i32) -> i32 {
    if a >= b { a } else { b }
}

// Explicit return: early exit from a loop
fn first_positive(numbers: &[i32]) -> Option<i32> {
    for &num in numbers {
        if num > 0 {
            return Some(num);  // early exit
        }
    }
    None  // implicit return if no positive number found
}
```

**What's happening here:**

1. `absolute_value` uses `if/else` as an expression. Both branches return `i32`. No `return` keyword needed because the entire `if/else` is the last (and only) expression in the function.
2. `max_of_two` does the same thing on a single line. When the logic is simple, this is readable and idiomatic.
3. `first_positive` takes a **slice** (`&[i32]`) — a reference to a contiguous sequence of `i32` values. This works with arrays of any length, unlike `[i32; 4]` which only works with length 4.
4. `return Some(num)` exits the function immediately when a positive number is found. `None` at the end is the implicit return for the "not found" case. `Option<i32>` is either `Some(value)` or `None` — Rust's way of representing "maybe a value, maybe not" without null pointers.
5. `for &num in numbers` destructures each reference — `num` is an `i32`, not an `&i32`. This is a pattern matching convenience.

What do you think this will print?

```
$ cargo run
abs(-5) = 5
abs(3) = 3
abs(0) = 0

max(10, 20) = 20
max(20, 10) = 20
max(5, 5) = 5

first_positive(&[-3, -1, 0, 4, 7]) = Some(4)
first_positive(&[-3, -1, -5]) = None
```

### Exercise 4: The Semicolon Trap

Edit `src/main.rs`:

```rust
fn main() {
    // Correct: returns i32
    let a = works();
    println!("works() = {}", a);

    // What happens if we add a semicolon to the return expression?
    // Uncomment the next line and broken() below to see the error:
    // let b = broken();

    // Functions that return ()
    let result = say_hello();
    println!("say_hello() returns: {:?}", result);

    // Semicolons in different positions
    let x = compute(10);
    println!("compute(10) = {}", x);
}

fn works() -> i32 {
    let value = 6;
    value * 7    // no semicolon: returns 42
}

// Uncomment this to see the compiler error:
// fn broken() -> i32 {
//     let value = 6;
//     value * 7;   // semicolon makes this a statement, block returns ()
// }

fn say_hello() {
    println!("Hello!");
    // implicit return: ()
}

fn compute(input: i32) -> i32 {
    // Multiple statements, then a final expression
    let step_one = input * 2;
    let step_two = step_one + 5;
    let step_three = step_two / 3;
    step_three   // the final result, no semicolon
}
```

**What's happening here:**

1. `works()` returns `42` because `value * 7` is an expression without a semicolon.
2. The commented-out `broken()` demonstrates the most common Rust beginner mistake. Adding a semicolon to `value * 7;` turns it into a statement. The block now returns `()`, but the function signature promises `-> i32`. This is a type mismatch.
3. `say_hello()` has no `-> Type`, so it returns `()`. The semicolons on its statements are fine because nothing needs to be returned.
4. `compute()` has three `let` statements (with semicolons) followed by one expression (without a semicolon). Only the last line produces the return value.

What do you think this will print?

```
$ cargo run
works() = 42
Hello!
say_hello() returns: ()
compute(10) = 8
```

Let us verify the `compute` math: `10 * 2 = 20`, `20 + 5 = 25`, `25 / 3 = 8` (integer division truncates).

Now, temporarily uncomment `broken()` and `let b = broken();` to see the error:

```
error[E0308]: mismatched types
  --> src/main.rs:XX:24
   |
XX | fn broken() -> i32 {
   |                --- expected `i32` because of return type
...
XX |     value * 7;
   |              - help: remove this semicolon to return this value
   |
   = note: expected type `i32`
              found unit type `()`
```

The compiler tells you exactly what is wrong and how to fix it: "remove this semicolon to return this value." Re-comment the lines after observing the error.

### Exercise 5: Functions Returning Tuples

Edit `src/main.rs`:

```rust
fn main() {
    let (min, max) = min_max(&[38, 12, 77, 5, 42, 91, 23]);
    println!("min: {}, max: {}", min, max);

    let stats = array_stats(&[10, 20, 30, 40, 50]);
    println!("\nArray: [10, 20, 30, 40, 50]");
    println!("  Count: {}", stats.0);
    println!("  Sum: {}", stats.1);
    println!("  Average: {:.2}", stats.2);

    let (whole, fractional) = split_float(3.14159);
    println!("\nsplit_float(3.14159):");
    println!("  Whole: {}", whole);
    println!("  Fractional: {:.5}", fractional);

    let (whole, fractional) = split_float(-2.718);
    println!("split_float(-2.718):");
    println!("  Whole: {}", whole);
    println!("  Fractional: {:.3}", fractional);
}

fn min_max(values: &[i32]) -> (i32, i32) {
    let mut min = values[0];
    let mut max = values[0];

    for &val in &values[1..] {
        if val < min {
            min = val;
        }
        if val > max {
            max = val;
        }
    }

    (min, max)  // return a tuple
}

fn array_stats(values: &[i32]) -> (usize, i32, f64) {
    let count = values.len();
    let mut sum = 0;

    for &val in values {
        sum += val;
    }

    let average = sum as f64 / count as f64;

    (count, sum, average)  // return a 3-element tuple
}

fn split_float(value: f64) -> (i64, f64) {
    let whole = value as i64;
    let fractional = value - whole as f64;
    (whole, fractional)
}
```

**What's happening here:**

1. `min_max` returns `(i32, i32)` — a tuple of two values. The caller destructures it with `let (min, max) = min_max(...)`.
2. `array_stats` returns `(usize, i32, f64)` — three values of different types. The caller accesses them with `.0`, `.1`, `.2` (dot notation without destructuring, to show both styles).
3. `split_float` separates a float into its whole and fractional parts. `value as i64` truncates toward zero, then subtraction recovers the fractional part.
4. `&values[1..]` slices from index 1 to the end. `for &val in &values[1..]` iterates over that slice, destructuring each reference.
5. All functions use implicit returns — the last expression (a tuple literal) has no semicolon.

What do you think this will print?

```
$ cargo run
min: 5, max: 91

Array: [10, 20, 30, 40, 50]
  Count: 5
  Sum: 150
  Average: 30.00

split_float(3.14159):
  Whole: 3
  Fractional: 0.14159
split_float(-2.718):
  Whole: -2
  Fractional: -0.718
```

## Common Mistakes

### Adding a Semicolon to the Return Expression

```rust
fn add(a: i32, b: i32) -> i32 {
    a + b;
}
```

```
error[E0308]: mismatched types
 --> src/main.rs:1:28
  |
1 | fn add(a: i32, b: i32) -> i32 {
  |                            --- expected `i32` because of return type
2 |     a + b;
  |          - help: remove this semicolon to return this value
```

**Why:** The semicolon turns `a + b` into a statement that returns `()`. The function expects `i32`.
**Fix:** Remove the semicolon: `a + b`

### Forgetting Type Annotations on Parameters

```rust
fn add(a, b) -> i32 {
    a + b
}
```

```
error: expected one of `:`, `@`, or `|`, found `,`
 --> src/main.rs:1:9
  |
1 | fn add(a, b) -> i32 {
  |         ^ expected one of `:`, `@`, or `|`
```

**Why:** Rust requires explicit types on all function parameters. Unlike `let` bindings, there is no inference for parameters. This is a design decision — function signatures are contracts, and ambiguous contracts cause bugs.
**Fix:** `fn add(a: i32, b: i32) -> i32`

### Mismatched Branch Types in if/else Expression

```rust
fn main() {
    let x = if true { 5 } else { "five" };
}
```

```
error[E0308]: `if` and `else` have incompatible types
 --> src/main.rs:2:38
  |
2 |     let x = if true { 5 } else { "five" };
  |                        -          ^^^^^^ expected integer, found `&str`
  |                        |
  |                        expected because of this
```

**Why:** When `if/else` is used as an expression, both branches must return the same type. The compiler infers `i32` from the first branch and rejects `&str` from the second.
**Fix:** Make both branches return the same type.

### Using return Without Semicolon in the Middle of a Function

```rust
fn check(x: i32) -> &'static str {
    if x > 0 {
        return "positive"
    }
    "non-positive"
}
```

This actually compiles and works, but it is misleading style. The `return "positive"` on its own (without a semicolon) looks like it might be the value of the `if` block rather than an early exit. The convention is to always use a semicolon after `return`:

```rust
fn check(x: i32) -> &'static str {
    if x > 0 {
        return "positive";
    }
    "non-positive"
}
```

## Verification

Run these commands from your project directory:

```
$ cargo check
    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.00s
```

```
$ cargo run
min: 5, max: 91

Array: [10, 20, 30, 40, 50]
  Count: 5
  Sum: 150
  Average: 30.00

split_float(3.14159):
  Whole: 3
  Fractional: 0.14159
split_float(-2.718):
  Whole: -2
  Fractional: -0.718
```

```
$ cargo build --release
    Finished `release` profile [optimized] target(s) in 0.00s
```

If all commands succeed with the expected output, you have completed this exercise.

## Summary

- **Key concepts**: `fn` declarations with typed parameters, `-> Type` return syntax, expressions vs statements, implicit return (last expression without semicolon), explicit `return` for early exits, blocks as expressions, semicolons converting expressions to statements
- **What you practiced**: Writing functions with parameters and return values, using if/else as expressions, returning tuples from functions, understanding the semicolon rule, reading compiler errors about type mismatches
- **Important to remember**: Almost everything in Rust is an expression. A semicolon discards the expression's value. The last expression in a block (without a semicolon) is the block's return value. Function parameters always need type annotations. All branches of an if/else expression must return the same type.

## What's Next

You now have the foundation: variables, types, and functions. The next major topic is **ownership** — Rust's core innovation that eliminates memory bugs at compile time without a garbage collector. It is the concept that makes Rust unique, and everything you have learned so far prepares you for it.

## Resources

- [The Rust Programming Language — Chapter 3.3: Functions](https://doc.rust-lang.org/book/ch03-03-how-functions-work.html)
- [Rust by Example — Functions](https://doc.rust-lang.org/rust-by-example/fn.html)
- [Rust by Example — Expressions](https://doc.rust-lang.org/rust-by-example/expression.html)
- [Rust Reference — Statements and Expressions](https://doc.rust-lang.org/reference/statements-and-expressions.html)
