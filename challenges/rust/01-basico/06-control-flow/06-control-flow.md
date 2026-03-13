# 6. Control Flow

**Difficulty**: Basico

## Prerequisites
- Exercise 01-05 completed (variables, basic types, functions)
- Familiarity with `let` bindings and type annotations
- Understanding of how functions return values

## Learning Objectives
After completing this exercise, you will be able to:
- Use `if/else` as expressions that return values
- Bind the result of an `if` expression to a variable with `let`
- Build infinite loops with `loop` and return values via `break`
- Iterate with `while` and `for` loops over ranges and collections
- Use labeled loops to break out of nested loops
- Apply basic `match` expressions with exhaustive pattern matching

## Concepts

### if/else Are Expressions, Not Statements

In most languages, `if` is a statement -- it does something but does not produce a value.
In Rust, `if` is an expression. It evaluates to a value, just like `2 + 3` evaluates to `5`.
This distinction matters because it lets you use `if` anywhere you need a value: in `let` bindings, function return positions, even inside other expressions.

```rust
fn main() {
    let temperature = 35;

    // if/else used as a statement (for side effects)
    if temperature > 30 {
        println!("It's hot");
    } else {
        println!("It's fine");
    }

    // if/else used as an expression (producing a value)
    let description = if temperature > 30 {
        "hot"
    } else {
        "fine"
    };

    println!("The weather is {description}");
}
```

Notice: when `if` is used as an expression, there are **no semicolons** after `"hot"` and `"fine"` inside the braces -- they are the return values of each branch. The semicolon goes after the closing brace and `}` to end the `let` statement.

Both branches **must return the same type**. Rust will not let you return a `&str` from one branch and an `i32` from another.

### loop: Infinite Loops That Return Values

`loop` creates an infinite loop. You exit it with `break`. Unlike `while true` in other languages, `loop` can **return a value** through `break`.

Why does this matter? Because sometimes you need to retry an operation until it succeeds and then use the result. `loop` with `break value` handles this cleanly.

```rust
fn main() {
    let mut counter = 0;

    let result = loop {
        counter += 1;

        if counter == 10 {
            break counter * 2; // returns 20 from the loop
        }
    };

    println!("Result: {result}");
}
```

The value after `break` becomes the value of the entire `loop` expression. This is unique to Rust -- C and Python loops cannot do this.

### while Loops

`while` works exactly as you would expect: it checks a condition before each iteration.

```rust
fn main() {
    let mut countdown = 5;

    while countdown > 0 {
        println!("{countdown}...");
        countdown -= 1;
    }

    println!("Liftoff!");
}
```

Unlike `loop`, a `while` loop cannot return a value through `break` because the compiler cannot guarantee the body ever executes.

### for Loops and Ranges

`for` is the workhorse of iteration in Rust. It works with anything that implements the `Iterator` trait (you will learn about traits later -- for now, think of it as "anything that can produce a sequence of values").

```rust
fn main() {
    // Range: start..end (exclusive end)
    for i in 1..5 {
        println!("{i}");
    }
    // Prints: 1, 2, 3, 4

    // Inclusive range: start..=end
    for i in 1..=5 {
        println!("{i}");
    }
    // Prints: 1, 2, 3, 4, 5

    // Iterating over an array
    let colors = ["red", "green", "blue"];
    for color in colors {
        println!("{color}");
    }

    // Reverse a range
    for i in (1..=5).rev() {
        println!("{i}");
    }
    // Prints: 5, 4, 3, 2, 1
}
```

Prefer `for` over `while` when iterating over a collection. `for` cannot go out of bounds; `while` with a manual index can.

### Labeled Loops

When you have nested loops, `break` exits the innermost loop by default. Labels let you target an outer loop.

A label is an identifier prefixed with a single quote: `'outer`, `'inner`, etc.

```rust
fn main() {
    let mut found = false;

    'outer: for x in 0..10 {
        for y in 0..10 {
            if x + y == 15 && x * y == 56 {
                println!("Found: x={x}, y={y}");
                found = true;
                break 'outer; // exits the outer loop
            }
        }
    }

    if !found {
        println!("No solution found");
    }
}
```

Without `'outer`, the `break` would only exit the inner `for y` loop, and the outer loop would keep running.

### match: Exhaustive Pattern Matching

`match` is like `switch` in C or Java, but with a critical difference: it **must be exhaustive**. You must handle every possible value. The compiler enforces this.

```rust
fn main() {
    let code = 404;

    let status = match code {
        200 => "OK",
        301 => "Moved Permanently",
        404 => "Not Found",
        500 => "Internal Server Error",
        _ => "Unknown", // _ matches everything else
    };

    println!("{code}: {status}");
}
```

The `_` is a catch-all pattern. Without it, the compiler would reject this code because `code` is an `i32` with billions of possible values, and you only listed four.

`match` is also an expression -- it returns a value, just like `if`.

## Exercises

### Exercise 1: if as an Expression

This exercise shows how `if/else` produces a value that you can bind directly.

Create a new project:

```
$ cargo new control_flow
$ cd control_flow
```

Create `src/main.rs`:

```rust
fn classify_number(n: i32) -> &'static str {
    let classification = if n > 0 {
        "positive"
    } else if n < 0 {
        "negative"
    } else {
        "zero"
    };

    classification
}

fn main() {
    let numbers = [42, -7, 0, 100, -1];

    for n in numbers {
        println!("{n} is {}", classify_number(n));
    }
}
```

**What's happening here:**
1. `classify_number` uses `if/else if/else` as an expression to produce a `&'static str`.
2. Each branch returns a string literal -- no `return` keyword needed, no semicolons inside the branches.
3. The result is bound to `classification` and then returned from the function.
4. In `main`, we iterate over an array of numbers and classify each one.

What do you think this will print? Try to predict before running.

```
$ cargo run
42 is positive
-7 is negative
0 is zero
100 is positive
-1 is negative
```

### Exercise 2: loop With break Values

This exercise simulates retrying a computation until a condition is met, using `loop` to return the final result.

Replace `src/main.rs`:

```rust
fn find_first_multiple(base: u32, threshold: u32) -> u32 {
    let mut current = base;

    loop {
        if current >= threshold {
            break current;
        }
        current += base;
    }
}

fn main() {
    let result = find_first_multiple(7, 50);
    println!("First multiple of 7 >= 50: {result}");

    let result = find_first_multiple(13, 100);
    println!("First multiple of 13 >= 100: {result}");

    let result = find_first_multiple(3, 1);
    println!("First multiple of 3 >= 1: {result}");
}
```

**What's happening here:**
1. `find_first_multiple` uses `loop` to keep adding `base` to `current` until it reaches or exceeds `threshold`.
2. `break current` returns the value of `current` from the loop.
3. The function's return type is `u32`, and `loop` with `break value` satisfies that.

What do you think this will print?

```
$ cargo run
First multiple of 7 >= 50: 56
First multiple of 13 >= 100: 104
First multiple of 3 >= 1: 3
```

### Exercise 3: Labeled Loops and Nested Iteration

This exercise searches a 2D grid for a specific condition using labeled loops.

Replace `src/main.rs`:

```rust
fn main() {
    let matrix = [
        [1, 2, 3, 4],
        [5, 6, 7, 8],
        [9, 10, 11, 12],
    ];

    let target = 7;
    let mut position = None;

    'rows: for (row_idx, row) in matrix.iter().enumerate() {
        for (col_idx, &value) in row.iter().enumerate() {
            if value == target {
                position = Some((row_idx, col_idx));
                break 'rows;
            }
        }
    }

    match position {
        Some((row, col)) => println!("Found {target} at row {row}, col {col}"),
        None => println!("{target} not found"),
    }
}
```

**What's happening here:**
1. `matrix.iter().enumerate()` yields pairs of `(index, &element)` for each row.
2. Inside the inner loop, `&value` destructures the reference so `value` is a plain `i32`.
3. `break 'rows` exits the outer loop immediately when we find the target.
4. `position` uses `Option` -- either `Some((row, col))` or `None`. You will learn `Option` in depth later; for now, think of it as a value that may or may not be present.
5. The `match` at the end handles both cases. Try changing `target` to `99` to see the `None` branch.

```
$ cargo run
Found 7 at row 1, col 2
```

### Exercise 4: match With Multiple Patterns

This exercise demonstrates `match` with pattern combinations, guards, and the catch-all arm.

Replace `src/main.rs`:

```rust
fn describe_http_status(code: u16) -> &'static str {
    match code {
        200 => "OK",
        201 => "Created",
        204 => "No Content",
        301 | 302 => "Redirect",           // multiple patterns with |
        400 => "Bad Request",
        401 | 403 => "Auth Error",
        404 => "Not Found",
        500..=599 => "Server Error",        // range pattern
        code if code < 100 => "Invalid",    // match guard
        _ => "Other",
    }
}

fn main() {
    let codes = [200, 301, 302, 404, 503, 50, 999];

    for code in codes {
        println!("{code} -> {}", describe_http_status(code));
    }
}
```

**What's happening here:**
1. `301 | 302` matches either value -- the `|` means "or" inside patterns.
2. `500..=599` matches any value in the inclusive range 500 to 599.
3. `code if code < 100` is a **match guard**: it binds the value to `code` and then checks an additional condition.
4. `_` catches everything not matched above.
5. The compiler guarantees every possible `u16` value is covered.

```
$ cargo run
200 -> OK
301 -> Redirect
302 -> Redirect
404 -> Not Found
503 -> Server Error
50 -> Invalid
999 -> Other
```

### Exercise 5: Combining Control Flow

This exercise ties everything together. We will build a simple number guessing evaluator.

Replace `src/main.rs`:

```rust
fn evaluate_guess(secret: i32, guess: i32) -> &'static str {
    let difference = (secret - guess).abs();

    match difference {
        0 => "correct",
        1..=5 => "very close",
        6..=15 => "warm",
        16..=50 => "cold",
        _ => "freezing",
    }
}

fn main() {
    let secret = 42;
    let guesses = [42, 40, 50, 30, 100, 1];

    for guess in guesses {
        let result = evaluate_guess(secret, guess);

        let arrow = if result == "correct" {
            "=="
        } else if guess < secret {
            "< "
        } else {
            "> "
        };

        println!("Guess {guess:>3} {arrow} {secret}: {result}");
    }

    // Count how many guesses were "warm" or better
    let mut good_guesses = 0_u32;
    for guess in guesses {
        let result = evaluate_guess(secret, guess);
        match result {
            "correct" | "very close" | "warm" => good_guesses += 1,
            _ => {}
        }
    }

    println!("\nGood guesses: {good_guesses} out of {}", guesses.len());
}
```

**What's happening here:**
1. `evaluate_guess` uses `match` with ranges to classify how close a guess is.
2. The `if/else` expression determines which arrow symbol to display.
3. `{guess:>3}` right-aligns the guess in a 3-character-wide field for neat output.
4. The second loop uses `match` with `|` (or-patterns) to count good guesses.
5. `_ => {}` is the "do nothing" arm -- required because `match` must be exhaustive.

```
$ cargo run
Guess  42 == 42: correct
Guess  40 <  42: very close
Guess  50 >  42: warm
Guess  30 <  42: cold
Guess 100 >  42: freezing
Guess   1 <  42: freezing

Good guesses: 3 out of 6
```

## Common Mistakes

### Mismatched Types in if Branches

```rust
fn main() {
    let x = 5;
    let value = if x > 0 { "positive" } else { -1 };
}
```

```
error[E0308]: `if` and `else` have incompatible types
 --> src/main.rs:3:50
  |
3 |     let value = if x > 0 { "positive" } else { -1 };
  |                             ----------          ^^ expected `&str`, found `i32`
  |                             |
  |                             expected because of this
```

Both branches of an `if` expression must return the same type. The compiler infers the type from the first branch and checks the second. To fix this, decide on a single type:

```rust
let value = if x > 0 { "positive" } else { "negative" };
```

### Forgetting the Catch-All in match

```rust
fn main() {
    let n: i32 = 5;
    let label = match n {
        1 => "one",
        2 => "two",
    };
}
```

```
error[E0004]: non-exhaustive patterns: `i32::MIN..=0_i32` and `3_i32..=i32::MAX` not covered
```

`match` must cover every possible value. Add `_ => "other"` to handle the rest.

### Using break Value in while

```rust
fn main() {
    let mut i = 0;
    let result = while i < 10 {
        i += 1;
        if i == 5 {
            break i; // error!
        }
    };
}
```

```
error[E0571]: `break` with value from a `while` loop
```

Only `loop` supports returning values through `break`. `while` and `for` loops always evaluate to `()` (the unit type). Restructure with `loop`:

```rust
fn main() {
    let mut i = 0;
    let result = loop {
        i += 1;
        if i == 5 {
            break i;
        }
    };
    println!("{result}");
}
```

## Verification

Run these commands to verify everything works:

```
$ cargo build
   Compiling control_flow v0.1.0
    Finished `dev` profile

$ cargo run
Guess  42 == 42: correct
Guess  40 <  42: very close
Guess  50 >  42: warm
Guess  30 <  42: cold
Guess 100 >  42: freezing
Guess   1 <  42: freezing

Good guesses: 3 out of 6

$ cargo clippy
    Finished `dev` profile
```

## Summary

- **Key concepts**: `if/else` as expressions, `loop` with `break` values, `for` with ranges and iterators, labeled loops, exhaustive `match`
- **What you practiced**: binding `if` results to variables, returning values from `loop`, iterating over arrays with `for`, using labeled breaks in nested loops, pattern matching with ranges, guards, and or-patterns
- **Important to remember**: both `if` branches must return the same type; `match` must be exhaustive; only `loop` can return values via `break`; prefer `for` over `while` for collection iteration

## What's Next

In the next exercise, you will tackle **Ownership** -- the concept that makes Rust unique among programming languages. Ownership is how Rust achieves memory safety without a garbage collector. Everything you have learned about variables, types, and control flow builds toward understanding ownership.

## Resources

- [The Rust Book -- Control Flow](https://doc.rust-lang.org/book/ch03-05-control-flow.html)
- [Rust Reference -- Expressions](https://doc.rust-lang.org/reference/expressions.html)
- [Rust by Example -- Flow of Control](https://doc.rust-lang.org/rust-by-example/flow_control.html)
