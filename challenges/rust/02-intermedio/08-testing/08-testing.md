# 8. Testing

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-ownership-and-borrowing, 02-structs-and-enums, 07-error-handling-patterns
- Comfortable writing functions and modules
- Basic understanding of `Result<T, E>`

## Learning Objectives

- Organize unit tests within `#[cfg(test)]` modules alongside source code
- Apply different assertion macros for different verification needs
- Build integration tests in the `tests/` directory
- Write documentation tests that serve as both examples and tests
- Apply test filtering, `#[ignore]`, and output control for efficient test workflows

## Concepts

### Why Tests in Rust Are Different

Most languages bolt testing on as an afterthought — external frameworks, test runners, mocking libraries. Rust bakes it into the language and toolchain. `cargo test` is built in. Test modules live right next to the code. Doc comments are tests. This means there is no excuse to skip testing, and no setup overhead to start.

### Unit Tests

Unit tests go in the same file as the code they test, inside a module annotated with `#[cfg(test)]`. This module is only compiled when running tests — it adds zero overhead to your release binary.

```rust
pub fn add(a: i32, b: i32) -> i32 {
    a + b
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_add_positive() {
        assert_eq!(add(2, 3), 5);
    }

    #[test]
    fn test_add_negative() {
        assert_eq!(add(-1, -1), -2);
    }
}
```

Key point: `use super::*` imports everything from the parent module — including private functions. Unit tests *can* test private internals. This is by design.

### Assertion Macros

- `assert!(expr)` — passes if `expr` is `true`
- `assert_eq!(left, right)` — passes if equal, prints both values on failure
- `assert_ne!(left, right)` — passes if not equal
- All three accept an optional format message: `assert!(x > 0, "expected positive, got {x}")`

### Result-Based Tests

Tests can return `Result<(), E>`, allowing you to use `?` instead of `.unwrap()`:

```rust
#[test]
fn test_parse() -> Result<(), Box<dyn std::error::Error>> {
    let num: i32 = "42".parse()?;
    assert_eq!(num, 42);
    Ok(())
}
```

### #[should_panic]

For testing that code panics under expected conditions:

```rust
#[test]
#[should_panic(expected = "index out of bounds")]
fn test_out_of_bounds() {
    let v = vec![1, 2, 3];
    let _ = v[99];
}
```

### Integration Tests

Integration tests live in a `tests/` directory at the crate root. Each file there is compiled as a separate crate, which means it can only access your crate's *public* API.

```
my_project/
  src/
    lib.rs
  tests/
    integration_test.rs
  Cargo.toml
```

```rust
// tests/integration_test.rs
use my_project::add;

#[test]
fn test_add_from_outside() {
    assert_eq!(add(10, 20), 30);
}
```

### Doc Tests

Code blocks in doc comments are compiled and run as tests:

```rust
/// Adds two numbers together.
///
/// ```
/// use my_project::add;
/// assert_eq!(my_project::add(2, 3), 5);
/// ```
pub fn add(a: i32, b: i32) -> i32 {
    a + b
}
```

Run with `cargo test --doc`.

### Test Control

- `#[ignore]` — skip a test unless explicitly requested (`cargo test -- --ignored`)
- `cargo test test_name` — run only tests matching a substring
- `cargo test -- --nocapture` — show `println!` output even for passing tests
- `cargo test -- --test-threads=1` — run tests sequentially (useful for shared resources)

## Exercises

### Project Setup

Create this structure. All exercises use a single project.

```
testing-exercise/
  src/
    lib.rs
    calculator.rs
    validator.rs
  tests/
    calculator_integration.rs
  Cargo.toml
```

```toml
# Cargo.toml
[package]
name = "testing-exercise"
version = "0.1.0"
edition = "2021"
```

### Exercise 1: Unit Tests with Assertions

Fill in the test module for a calculator.

```rust
// src/calculator.rs

pub struct Calculator {
    history: Vec<f64>,
}

impl Calculator {
    pub fn new() -> Self {
        Calculator { history: Vec::new() }
    }

    pub fn add(&mut self, a: f64, b: f64) -> f64 {
        let result = a + b;
        self.history.push(result);
        result
    }

    pub fn divide(&mut self, a: f64, b: f64) -> Result<f64, String> {
        if b == 0.0 {
            return Err("division by zero".to_string());
        }
        let result = a / b;
        self.history.push(result);
        Ok(result)
    }

    pub fn last_result(&self) -> Option<f64> {
        self.history.last().copied()
    }

    /// Clears history. Not part of public API — only testable from unit tests.
    fn clear_history(&mut self) {
        self.history.clear();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_add_integers() {
        let mut calc = Calculator::new();
        // TODO: assert that add(2.0, 3.0) returns 5.0
    }

    #[test]
    fn test_add_negative() {
        let mut calc = Calculator::new();
        // TODO: assert that add(-1.0, -1.0) returns -2.0
    }

    #[test]
    fn test_divide_ok() {
        let mut calc = Calculator::new();
        // TODO: assert that divide(10.0, 3.0) returns Ok with approximate value 3.333...
        // Hint: for floating point, check (result - expected).abs() < epsilon
    }

    #[test]
    fn test_divide_by_zero() {
        let mut calc = Calculator::new();
        // TODO: assert that divide(1.0, 0.0) returns Err
        // Check the error message contains "division by zero"
    }

    #[test]
    fn test_history_tracking() {
        let mut calc = Calculator::new();
        // TODO: perform two operations, verify last_result returns the second one
    }

    #[test]
    fn test_empty_history() {
        let calc = Calculator::new();
        // TODO: assert last_result is None on a fresh calculator
    }

    #[test]
    fn test_clear_history_private() {
        // TODO: Test the private clear_history method.
        // This is possible in unit tests because of `use super::*`.
        // Verify history is empty after clearing.
    }

    // TODO: Add a Result-based test that uses ? with divide
    #[test]
    fn test_divide_with_result() -> Result<(), String> {
        todo!()
    }
}
```

```rust
// src/lib.rs
pub mod calculator;
pub mod validator;
```

### Exercise 2: Testing Error Conditions

Write tests for a validator module that should catch various bad inputs.

```rust
// src/validator.rs

#[derive(Debug, PartialEq)]
pub enum ValidationError {
    TooShort(usize),
    TooLong(usize),
    InvalidChar(char),
    Empty,
}

pub fn validate_username(username: &str) -> Result<(), ValidationError> {
    if username.is_empty() {
        return Err(ValidationError::Empty);
    }
    if username.len() < 3 {
        return Err(ValidationError::TooShort(username.len()));
    }
    if username.len() > 20 {
        return Err(ValidationError::TooLong(username.len()));
    }
    if let Some(c) = username.chars().find(|c| !c.is_alphanumeric() && *c != '_') {
        return Err(ValidationError::InvalidChar(c));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    // TODO: Write tests for each of these cases:
    // 1. A valid username passes (returns Ok(()))
    // 2. Empty string returns Empty
    // 3. "ab" returns TooShort(2)
    // 4. A 21-char string returns TooLong(21)
    // 5. "user@name" returns InvalidChar('@')
    // 6. Underscores are allowed: "valid_name" passes
    // 7. Edge case: exactly 3 chars passes
    // 8. Edge case: exactly 20 chars passes

    // Hint: Use assert_eq! with the exact error variant.
    // Example:
    // assert_eq!(validate_username(""), Err(ValidationError::Empty));
}
```

### Exercise 3: Integration Tests

Test the public API from outside the crate.

```rust
// tests/calculator_integration.rs

use testing_exercise::calculator::Calculator;

#[test]
fn test_full_calculation_workflow() {
    // TODO: Create a calculator, perform several operations,
    // verify the final state through the public API only.
    // Note: you CANNOT call clear_history here — it is private.
}

#[test]
fn test_error_recovery() {
    // TODO: Perform a divide-by-zero, then verify the calculator
    // still works correctly for subsequent operations.
}

#[test]
#[ignore]
fn test_large_history() {
    // TODO: Perform 1_000_000 operations and verify last_result.
    // This is marked #[ignore] because it is slow.
    // Run with: cargo test -- --ignored
}
```

### Exercise 4: Doc Tests

Add doc comments with testable examples to the calculator.

```rust
// Add these doc comments to the Calculator methods in src/calculator.rs

/// Creates a new calculator with empty history.
///
/// ```
/// use testing_exercise::calculator::Calculator;
/// let calc = Calculator::new();
/// assert_eq!(calc.last_result(), None);
/// ```
// TODO: Add doc tests for `add` and `divide` methods.
// The divide doc test should show both the success and error cases.
// Remember: doc tests run as if they were a separate crate,
// so you need full paths like `testing_exercise::calculator::Calculator`.
```

### Exercise 5: Test Helpers and Organization

Build a test helper to reduce duplication.

```rust
// src/calculator.rs — add to the #[cfg(test)] module

#[cfg(test)]
mod tests {
    use super::*;

    // TODO: Create a helper function that builds a Calculator
    // pre-loaded with a specific history of operations.
    fn calculator_with_history(operations: &[(f64, f64)]) -> Calculator {
        todo!()
    }

    // TODO: Create a helper macro that asserts a float is approximately equal
    // macro_rules! assert_approx_eq {
    //     ($left:expr, $right:expr) => { ... };
    //     ($left:expr, $right:expr, $epsilon:expr) => { ... };
    // }

    #[test]
    fn test_with_preloaded_history() {
        let calc = calculator_with_history(&[(1.0, 2.0), (3.0, 4.0)]);
        assert_eq!(calc.last_result(), Some(7.0));
    }

    #[test]
    fn test_floating_point_approx() {
        let mut calc = Calculator::new();
        let result = calc.divide(1.0, 3.0).unwrap();
        assert_approx_eq!(result, 0.333_333_333, 1e-6);
    }
}
```

### Try It Yourself

1. **Property-based thinking**: Write a test that verifies `add(a, b) == add(b, a)` for 100 random pairs. You do not need a property-testing library — just use a loop with varied inputs.

2. **Test-driven development**: Write tests *first* for a `validate_email` function, then implement the function to make them pass. Start with at least 6 test cases covering valid emails, missing `@`, missing domain, etc.

3. **Conditional compilation**: Add a `#[cfg(test)]` block that defines a mock version of an external dependency. For example, create a trait `TimeProvider` with a real impl and a test impl that always returns a fixed time.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Comparing floats with `assert_eq!` | Intermittent failures | Use epsilon comparison |
| `#[test]` outside `#[cfg(test)]` module | Tests compiled into release binary | Always wrap in `#[cfg(test)] mod tests` |
| Testing private functions from integration tests | Compile error | Private functions are unit-test only |
| No `use super::*` in test module | Cannot find items | Add the import |
| `#[should_panic]` without `expected` | Passes on wrong panic | Always include `expected` substring |
| Forgetting `pub` on items you want to integration-test | Compile error in `tests/` | Make the API public |

## Verification

```bash
# Run all tests
cargo test

# Run only unit tests
cargo test --lib

# Run only integration tests
cargo test --test calculator_integration

# Run only doc tests
cargo test --doc

# Run ignored tests
cargo test -- --ignored

# Run tests matching a pattern
cargo test test_divide

# See output from passing tests
cargo test -- --nocapture
```

All tests should pass. The `#[ignore]` test should be skipped by default and pass when run explicitly.

## Summary

Rust gives you three kinds of tests, each with a purpose:

- **Unit tests** live in `#[cfg(test)]` modules alongside the code. They can access private internals. Use them for testing individual functions and edge cases.
- **Integration tests** live in `tests/` and see only the public API. They test the crate as a consumer would use it.
- **Doc tests** live in `///` comments and serve double duty as documentation and tests. They verify your examples actually work.

`cargo test` runs all three by default. Use filtering and `#[ignore]` to keep your fast feedback loop tight during development.

## What's Next

- Exercise 09 covers modules and visibility, where `pub` and `pub(crate)` directly affect what integration tests can access
- Later exercises on async and concurrency will introduce testing patterns for async code

## Resources

- [The Rust Book, Chapter 11: Writing Automated Tests](https://doc.rust-lang.org/book/ch11-00-testing.html)
- [Rust by Example: Testing](https://doc.rust-lang.org/rust-by-example/testing.html)
- [cargo test documentation](https://doc.rust-lang.org/cargo/commands/cargo-test.html)
- [The `#[cfg(test)]` attribute](https://doc.rust-lang.org/reference/conditional-compilation.html)
