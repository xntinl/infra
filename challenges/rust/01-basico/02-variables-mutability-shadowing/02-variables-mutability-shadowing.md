# 2. Variables, Mutability, and Shadowing

**Difficulty**: Basico

## Prerequisites

- Completed exercise 1 (Hello Rust and Cargo)
- Ability to create and run a Rust project with `cargo new` and `cargo run`

## Learning Objectives

After completing this exercise, you will be able to:

- Declare variables with `let` and explain why they are immutable by default
- Use `mut` to make variables mutable when necessary
- Define compile-time constants with `const` and static variables with `static`
- Apply shadowing to rebind a variable name with a new value or type
- Follow Rust's naming conventions (`snake_case`, `SCREAMING_SNAKE_CASE`)

## Concepts

### Why Variables Are Immutable by Default

In most languages (JavaScript, Python, Java), variables are mutable by default and you opt into immutability (`const`, `final`, `frozen`). Rust inverts this: variables are **immutable by default** and you opt into mutability with `mut`.

Why? Because mutable state is the root cause of most bugs in concurrent and complex programs. If a value cannot change, you cannot accidentally change it. The compiler enforces this — if you try to reassign an immutable variable, the code will not compile. This is not a convention or a linting rule. It is a hard guarantee.

### let Bindings

`let` introduces a **binding** — it associates a name with a value. Rust developers say "binding" rather than "variable" because the name is bound to a value, and by default that binding cannot be changed.

```rust
let x = 5;
```

The compiler **infers** the type from the value. Here `5` is an integer literal, so `x` gets type `i32` (a 32-bit signed integer — Rust's default integer type). You can also annotate the type explicitly:

```rust
let x: i32 = 5;
```

Both are identical. Use explicit annotations when the compiler cannot infer the type, or when you want to be clear about your intent.

### The mut Keyword

When you genuinely need a value to change — a counter, a buffer, an accumulator — mark it `mut`:

```rust
let mut counter = 0;
counter = counter + 1;
```

The `mut` keyword is an explicit signal to anyone reading the code: "this value will change." It makes mutation visible at the declaration site, not buried somewhere deep in the logic.

### Constants and Static Variables

**Constants** (`const`) are values known at compile time. They are inlined wherever they are used — the compiler replaces the name with the literal value. They must have an explicit type annotation:

```rust
const MAX_RETRIES: u32 = 5;
```

**Static variables** (`static`) have a fixed memory address for the entire program lifetime. They are similar to global variables in C:

```rust
static APP_NAME: &str = "my-app";
```

The key differences:

| | `let` | `const` | `static` |
|---|---|---|---|
| Mutable | With `mut` | Never | With `unsafe` only |
| Type annotation | Optional | Required | Required |
| Evaluated at | Runtime | Compile time | Compile time |
| Memory | Stack | Inlined (no address) | Fixed address |
| Scope | Block | Any scope | Any scope |

Use `const` for values that never change and are known at compile time (config limits, mathematical constants). Use `static` only when you need a fixed memory address (rare in application code). Use `let` for everything else.

### Shadowing

Shadowing lets you reuse a variable name by declaring a new `let` binding with the same name. The new binding **shadows** (hides) the previous one:

```rust
let x = 5;
let x = x + 1; // shadows the previous x
```

This is not mutation — it creates an entirely new variable that happens to have the same name. The key difference from `mut`: shadowing lets you **change the type**:

```rust
let spaces = "   ";       // &str
let spaces = spaces.len(); // usize — different type, same name
```

With `mut`, this would be a compiler error because you cannot change a variable's type. Shadowing is useful when you transform a value through a pipeline and the intermediate names are meaningless.

### Naming Conventions

Rust has strong conventions enforced by compiler warnings:

- **Variables and functions**: `snake_case` — `let user_count = 0;`, `fn calculate_total()`
- **Constants and statics**: `SCREAMING_SNAKE_CASE` — `const MAX_SIZE: usize = 100;`
- **Types and traits**: `PascalCase` — `struct UserAccount`, `trait Serialize`

The compiler warns you if you deviate. These are not suggestions — the entire Rust ecosystem follows them.

## Exercises

### Exercise 1: Immutability by Default

Create a new project:

```
$ cargo new variables-lab
$ cd variables-lab
```

Edit `src/main.rs`:

```rust
fn main() {
    let language = "Rust";
    let year = 2015;

    println!("{} was first released in {}", language, year);
    println!("Current language: {}", language);
}
```

**What's happening here:**

1. `let language = "Rust"` binds the name `language` to the string literal `"Rust"`. The type `&str` is inferred.
2. `let year = 2015` binds `year` to the integer `2015`. The type `i32` is inferred.
3. Neither variable is `mut`, so neither can be reassigned.

What do you think this will print?

```
$ cargo run
Rust was first released in 2015
Current language: Rust
```

Now try to reassign `language`. Add this line before the second `println!`:

```rust
fn main() {
    let language = "Rust";
    let year = 2015;

    println!("{} was first released in {}", language, year);

    language = "C++";

    println!("Current language: {}", language);
}
```

```
$ cargo check
error[E0384]: cannot assign twice to immutable variable `language`
 --> src/main.rs:7:5
  |
2 |     let language = "Rust";
  |         --------
  |         |
  |         first assignment to `language`
  |         help: consider making this binding mutable: `mut language`
...
7 |     language = "C++";
  |     ^^^^^^^^^^^^^^^^ cannot assign twice to immutable variable
```

The compiler refuses. It even suggests the fix: add `mut`. But do not add it yet — we will explore that next.

Revert `src/main.rs` to the working version before continuing.

### Exercise 2: Opting Into Mutability

Edit `src/main.rs`:

```rust
fn main() {
    let mut count = 0;
    println!("Initial count: {}", count);

    count = 1;
    println!("After first increment: {}", count);

    count = 2;
    println!("After second increment: {}", count);

    count += 1;
    println!("After += 1: {}", count);
}
```

**What's happening here:**

1. `let mut count = 0` declares a mutable binding. The `mut` keyword allows reassignment.
2. `count = 1` replaces the value. This is allowed because of `mut`.
3. `count += 1` is shorthand for `count = count + 1`. This works for all arithmetic operators (`-=`, `*=`, `/=`, `%=`).

What do you think this will print?

```
$ cargo run
Initial count: 0
After first increment: 1
After second increment: 2
After += 1: 3
```

Note: `mut` allows changing the value but not the type. Try adding `count = "three";` and you will get a type mismatch error. A mutable `i32` can hold any `i32` value, but it is always an `i32`.

### Exercise 3: Constants and Statics

Edit `src/main.rs`:

```rust
const MAX_CONNECTIONS: u32 = 100;
const TIMEOUT_SECONDS: u32 = 30;
static VERSION: &str = "1.0.0";

fn main() {
    println!("App version: {}", VERSION);
    println!("Max connections: {}", MAX_CONNECTIONS);
    println!("Timeout: {}s", TIMEOUT_SECONDS);

    let current_connections = 42;
    let remaining = MAX_CONNECTIONS - current_connections;
    println!("Remaining capacity: {}/{}", remaining, MAX_CONNECTIONS);
}
```

**What's happening here:**

1. `const MAX_CONNECTIONS: u32 = 100` — a compile-time constant. The type annotation `: u32` is **required** for constants (the compiler will not infer it). The naming convention is `SCREAMING_SNAKE_CASE`.
2. `static VERSION: &str = "1.0.0"` — a static variable with a fixed memory address. Also requires a type annotation.
3. Both `const` and `static` are declared outside `main` here, making them accessible throughout the file. They can also be declared inside functions, but that is less common.

What do you think this will print?

```
$ cargo run
App version: 1.0.0
Max connections: 100
Timeout: 30s
Remaining capacity: 58/100
```

### Exercise 4: Shadowing

Edit `src/main.rs`:

```rust
fn main() {
    // Shadowing: same name, new binding
    let x = 5;
    println!("x = {}", x);

    let x = x + 1;
    println!("x after shadowing with x + 1 = {}", x);

    let x = x * 2;
    println!("x after shadowing with x * 2 = {}", x);

    // Shadowing allows type change
    let input = "   hello   ";
    println!("input as string: '{}'", input);

    let input = input.trim();
    println!("input after trim: '{}'", input);

    let input = input.len();
    println!("input as length: {}", input);

    // Shadowing in inner blocks
    let y = 10;
    {
        let y = y + 20;
        println!("y inside block: {}", y);
    }
    println!("y outside block: {}", y);
}
```

**What's happening here:**

1. Each `let x = ...` creates a **new** variable that shadows the previous one. The old `x` still exists in memory but is inaccessible by name.
2. `let input` is used three times with three different types: `&str`, then `&str` (trimmed), then `usize` (the length). Shadowing lets you reuse the name as the data transforms through a pipeline.
3. The inner block `{ let y = y + 20; ... }` creates a shadow that only lives inside the braces. Outside the block, the original `y` is visible again.

What do you think this will print?

```
$ cargo run
x = 5
x after shadowing with x + 1 = 6
x after shadowing with x * 2 = 12
input as string: '   hello   '
input after trim: 'hello'
input as length: 5
y inside block: 30
y outside block: 10
```

### Exercise 5: Shadowing vs mut — Know the Difference

Edit `src/main.rs`:

```rust
fn main() {
    // This works: shadowing allows type change
    let data = "42";
    let data = data.parse::<i32>().expect("not a number");
    let data = data * 2;
    println!("Shadowed data: {}", data);

    // This also works: mut allows value change (same type)
    let mut counter = 0;
    counter += 1;
    counter += 1;
    counter += 1;
    println!("Mutable counter: {}", counter);

    // Guideline: use shadowing for transformations,
    //            use mut for same-type incremental updates
    let raw_input = "  100  ";
    let trimmed = raw_input.trim();
    let parsed_value: i32 = trimmed.parse().expect("not a number");
    let doubled_value = parsed_value * 2;
    println!("Pipeline result: '{}' -> {} -> {}", raw_input, parsed_value, doubled_value);
}
```

**What's happening here:**

1. The first block uses shadowing to transform `data` from `&str` to `i32` and then multiply it. Each `let data` is a new variable.
2. The second block uses `mut` because `counter` stays the same type (`i32`) and gets updated incrementally.
3. The third block shows the cleaner approach for pipelines: give each step a descriptive name instead of shadowing. This is often more readable than shadowing when the transformations are complex.

What do you think this will print?

```
$ cargo run
Shadowed data: 84
Mutable counter: 3
Pipeline result: '  100  ' -> 100 -> 200
```

## Common Mistakes

### Trying to Mutate Without `mut`

```rust
fn main() {
    let x = 5;
    x = 10;
}
```

```
error[E0384]: cannot assign twice to immutable variable `x`
 --> src/main.rs:3:5
  |
2 |     let x = 5;
  |         -
  |         |
  |         first assignment to `x`
  |         help: consider making this binding mutable: `mut x`
3 |     x = 10;
  |     ^^^^^^ cannot assign twice to immutable variable
```

**Why:** Variables are immutable by default. The compiler is protecting you from accidental mutation.
**Fix:** Either use `let mut x = 5;` if you need mutation, or use `let x = 10;` (shadowing) if you want a new binding.

### Changing Type of a `mut` Variable

```rust
fn main() {
    let mut x = 5;
    x = "hello";
}
```

```
error[E0308]: mismatched types
 --> src/main.rs:3:9
  |
2 |     let mut x = 5;
  |                 - expected due to this value
3 |     x = "hello";
  |         ^^^^^^^ expected integer, found `&str`
```

**Why:** `mut` allows changing the value, not the type. `x` was inferred as `i32` and must remain `i32`.
**Fix:** Use shadowing instead: `let x = "hello";`

### Forgetting Type Annotation on `const`

```rust
const MAX_SIZE = 100;
```

```
error: missing type for `const` item
 --> src/main.rs:1:11
  |
1 | const MAX_SIZE = 100;
  |           ^ help: provide a type for the constant: `MAX_SIZE: i32`
```

**Why:** Constants require explicit type annotations. The compiler refuses to infer types for `const` and `static` — it forces you to be explicit about what type a global value has.
**Fix:** `const MAX_SIZE: u32 = 100;`

### Non-standard Naming

```rust
fn main() {
    let myVariable = 5;
    const max_size: u32 = 10;
}
```

```
warning: variable `myVariable` should have a snake case name
 --> src/main.rs:2:9
  |
2 |     let myVariable = 5;
  |         ^^^^^^^^^^ help: convert the identifier to snake case: `my_variable`

warning: constant `max_size` should have an upper case name
 --> src/main.rs:3:11
  |
3 |     const max_size: u32 = 10;
  |           ^^^^^^^^ help: convert the identifier to upper case: `MAX_SIZE`
```

**Why:** Rust enforces `snake_case` for variables and `SCREAMING_SNAKE_CASE` for constants. These are warnings, not errors, but the entire ecosystem follows them.
**Fix:** `let my_variable = 5;` and `const MAX_SIZE: u32 = 10;`

## Verification

Run these commands from your project directory:

```
$ cargo check
    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.00s
```

```
$ cargo run
Shadowed data: 84
Mutable counter: 3
Pipeline result: '  100  ' -> 100 -> 200
```

If both commands succeed with no warnings and the output matches, you have completed this exercise.

## Summary

- **Key concepts**: `let` bindings, immutability by default, `mut` for mutable variables, `const` for compile-time constants, `static` for fixed-address globals, shadowing
- **What you practiced**: declaring variables, mutating with `mut`, using constants, shadowing with type changes, reading compiler errors
- **Important to remember**: Immutability is the default for good reason — it prevents bugs. Use `mut` only when you need it. Use shadowing when you are transforming a value through different types. Constants always need type annotations.

## What's Next

We have been using types like `i32`, `&str`, and `usize` without explaining them in detail. In the next exercise, we will explore all of Rust's scalar types — integers, floats, booleans, and characters — and how the type system works.

## Resources

- [The Rust Programming Language — Chapter 3.1: Variables and Mutability](https://doc.rust-lang.org/book/ch03-01-variables-and-mutability.html)
- [Rust by Example — Variable Bindings](https://doc.rust-lang.org/rust-by-example/variable_bindings.html)
- [Rust Reference — Items: Constants and Statics](https://doc.rust-lang.org/reference/items/constant-items.html)
