# Option and Result

**Difficulty:** Basico
**Time:** 45-60 minutes
**Prerequisites:** Enums, pattern matching, structs, methods

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** that `Option<T>` replaces null and `Result<T, E>` replaces exceptions.
- **Understand** why `unwrap` is dangerous in production code and when the `?` operator is appropriate.
- **Apply** combinators (`map`, `and_then`, `unwrap_or`) to chain fallible operations without nesting.

## Concepts

### Why Rust has no null

Tony Hoare called null his "billion-dollar mistake." The problem is not the idea of absence -- it is that null is invisible. A value looks like it is there, but at runtime it is not, and your program crashes. Rust eliminates this by encoding absence in the type system:

```rust
enum Option<T> {
    Some(T),
    None,
}
```

If a function might not return a value, its return type is `Option<T>`. The compiler forces you to handle the `None` case before you can use the inner `T`. No surprises at runtime.

### Result for recoverable errors

When an operation can fail with a reason, Rust uses:

```rust
enum Result<T, E> {
    Ok(T),
    Err(E),
}
```

`T` is the success type, `E` is the error type. Like `Option`, you must handle both cases. There are no unchecked exceptions in Rust.

### unwrap and expect -- when NOT to use them

`unwrap()` extracts the inner value or panics. `expect("message")` does the same but with a custom panic message:

```rust
let val: Option<i32> = None;
val.unwrap(); // panics: "called `Option::unwrap()` on a `None` value"
```

Use them only in tests, prototypes, or when you have a logical proof that the value is present. In production code, prefer pattern matching, `?`, or combinators.

### The ? operator

Inside a function that returns `Option` or `Result`, the `?` operator short-circuits on `None` or `Err`:

```rust
fn parse_port(s: &str) -> Option<u16> {
    let trimmed = s.strip_prefix("port:")? ; // returns None if prefix missing
    trimmed.trim().parse().ok()               // converts Result to Option
}
```

This is syntactic sugar for a `match` that returns early on the failure case. It keeps code flat instead of deeply nested.

### Combinators

Combinators transform the inner value without unwrapping manually:

- `map(f)` -- apply `f` to the inner value if present: `Some(2).map(|x| x * 3)` gives `Some(6)`.
- `and_then(f)` -- like `map` but `f` itself returns an `Option` or `Result`, avoiding nesting: `Some("42").and_then(|s| s.parse().ok())`.
- `unwrap_or(default)` -- return the inner value or a fallback: `None.unwrap_or(0)` gives `0`.
- `unwrap_or_else(f)` -- like `unwrap_or` but the fallback is computed lazily.

### Converting between Option and Result

- `option.ok_or(err)` converts `Option<T>` to `Result<T, E>`.
- `result.ok()` converts `Result<T, E>` to `Option<T>`, discarding the error.
- `result.err()` converts `Result<T, E>` to `Option<E>`, discarding the success.

## Exercises

### Exercise 1 -- Option basics

What do you think this will print?

```rust
fn find_first_even(numbers: &[i32]) -> Option<i32> {
    for &n in numbers {
        if n % 2 == 0 {
            return Some(n);
        }
    }
    None
}

fn main() {
    let nums_a = vec![1, 3, 5, 8, 11];
    let nums_b = vec![1, 3, 5, 7];

    match find_first_even(&nums_a) {
        Some(n) => println!("Found even: {}", n),
        None => println!("No even numbers"),
    }

    match find_first_even(&nums_b) {
        Some(n) => println!("Found even: {}", n),
        None => println!("No even numbers"),
    }

    // Using if let for the common case
    if let Some(n) = find_first_even(&nums_a) {
        println!("First even (if let): {}", n);
    }
}
```

Predict, then verify with `cargo run`.

### Exercise 2 -- Result and the ? operator

Build a function that parses a key-value config line. Observe how `?` keeps the code flat:

```rust
#[derive(Debug)]
struct ConfigEntry {
    key: String,
    value: String,
}

#[derive(Debug)]
enum ParseError {
    MissingEquals,
    EmptyKey,
    EmptyValue,
}

fn parse_line(line: &str) -> Result<ConfigEntry, ParseError> {
    let trimmed = line.trim();
    let eq_pos = trimmed.find('=').ok_or(ParseError::MissingEquals)?;

    let key = trimmed[..eq_pos].trim();
    let value = trimmed[eq_pos + 1..].trim();

    if key.is_empty() {
        return Err(ParseError::EmptyKey);
    }
    if value.is_empty() {
        return Err(ParseError::EmptyValue);
    }

    Ok(ConfigEntry {
        key: key.to_string(),
        value: value.to_string(),
    })
}

fn main() {
    let lines = vec![
        "host = localhost",
        "port=8080",
        "no_equals_here",
        " = missing_key",
        "empty_value = ",
    ];

    for line in lines {
        match parse_line(line) {
            Ok(entry) => println!("OK: {:?}", entry),
            Err(e) => println!("ERR ({:?}): \"{}\"", e, line),
        }
    }
}
```

Trace through each line. Which ones succeed? Which error does each failure produce?

### Exercise 3 -- Combinators in practice

Replace manual matching with combinators. Predict the output:

```rust
fn parse_port(s: &str) -> Option<u16> {
    s.strip_prefix("port:")
        .and_then(|rest| rest.trim().parse().ok())
}

fn double_if_even(n: i32) -> Option<i32> {
    if n % 2 == 0 {
        Some(n * 2)
    } else {
        None
    }
}

fn main() {
    // map and unwrap_or
    let name: Option<&str> = Some("  Alice  ");
    let cleaned = name.map(|s| s.trim());
    println!("Cleaned: {:?}", cleaned);

    let missing: Option<&str> = None;
    let fallback = missing.unwrap_or("anonymous");
    println!("Fallback: {}", fallback);

    // and_then chains operations that themselves return Option
    let result = Some(4).and_then(double_if_even);
    println!("double_if_even(4): {:?}", result);

    let result = Some(3).and_then(double_if_even);
    println!("double_if_even(3): {:?}", result);

    // Parsing with strip + and_then
    println!("port:8080 => {:?}", parse_port("port:8080"));
    println!("port:abc  => {:?}", parse_port("port:abc"));
    println!("host:8080 => {:?}", parse_port("host:8080"));
}
```

### Exercise 4 -- Building a config parser

Combine everything into a small config parser that reads key-value pairs and extracts typed values:

```rust
use std::collections::HashMap;

#[derive(Debug)]
enum ConfigError {
    MissingKey(String),
    InvalidValue { key: String, reason: String },
}

struct Config {
    entries: HashMap<String, String>,
}

impl Config {
    fn from_text(text: &str) -> Self {
        let mut entries = HashMap::new();
        for line in text.lines() {
            let trimmed = line.trim();
            if trimmed.is_empty() || trimmed.starts_with('#') {
                continue; // skip blanks and comments
            }
            if let Some(eq_pos) = trimmed.find('=') {
                let key = trimmed[..eq_pos].trim().to_string();
                let value = trimmed[eq_pos + 1..].trim().to_string();
                if !key.is_empty() {
                    entries.insert(key, value);
                }
            }
        }
        Config { entries }
    }

    fn get(&self, key: &str) -> Result<&str, ConfigError> {
        self.entries
            .get(key)
            .map(|v| v.as_str())
            .ok_or_else(|| ConfigError::MissingKey(key.to_string()))
    }

    fn get_u16(&self, key: &str) -> Result<u16, ConfigError> {
        let raw = self.get(key)?;
        raw.parse().map_err(|_| ConfigError::InvalidValue {
            key: key.to_string(),
            reason: format!("'{}' is not a valid u16", raw),
        })
    }

    fn get_or(&self, key: &str, default: &str) -> String {
        self.entries
            .get(key)
            .cloned()
            .unwrap_or_else(|| default.to_string())
    }
}

fn main() {
    let text = "
# Server config
host = 127.0.0.1
port = 8080
max_connections = 100
debug = true
";

    let config = Config::from_text(text);

    // Successful lookups
    println!("host: {:?}", config.get("host"));
    println!("port: {:?}", config.get_u16("port"));

    // Missing key
    println!("timeout: {:?}", config.get("timeout"));

    // Default value
    println!("log_level: {}", config.get_or("log_level", "info"));

    // Invalid parse
    println!("debug as u16: {:?}", config.get_u16("debug"));
}
```

Before running, predict what each `println!` outputs, including whether it is `Ok(...)` or `Err(...)`.

### Exercise 5 -- Converting between Option and Result

Observe the conversion methods and trace the output:

```rust
fn lookup(data: &[(&str, i32)], key: &str) -> Option<i32> {
    data.iter()
        .find(|(k, _)| *k == key)
        .map(|(_, v)| *v)
}

fn require(data: &[(&str, i32)], key: &str) -> Result<i32, String> {
    lookup(data, key).ok_or_else(|| format!("key '{}' not found", key))
}

fn main() {
    let data = vec![("a", 1), ("b", 2), ("c", 3)];

    // Option -> Result with ok_or
    let found: Result<i32, &str> = lookup(&data, "b").ok_or("not found");
    let missing: Result<i32, &str> = lookup(&data, "z").ok_or("not found");
    println!("found:   {:?}", found);
    println!("missing: {:?}", missing);

    // Result -> Option with ok()
    let port: Result<u16, _> = "8080".parse();
    let bad: Result<u16, _> = "abc".parse();
    println!("port ok(): {:?}", port.ok());
    println!("bad ok():  {:?}", bad.ok());

    // Using require which converts internally
    println!("require a: {:?}", require(&data, "a"));
    println!("require z: {:?}", require(&data, "z"));
}
```

## Common Mistakes

**Using unwrap in production code:**

```rust
let port: u16 = config.get("port").unwrap().parse().unwrap();
// If either step fails, your server crashes at runtime
```

Fix: use `?` in a function returning `Result`, or handle both cases explicitly.

**Forgetting that ? requires a compatible return type:**

```
error[E0277]: the `?` operator can only be used in a function that returns
              `Result` or `Option`
```

Fix: change the function signature to return `Result<T, E>` or `Option<T>`. The `?` operator cannot be used in `main()` unless `main` returns `Result`.

**Chaining map when you need and_then:**

```rust
Some("42").map(|s| s.parse::<i32>().ok())
// This gives Option<Option<i32>> -- nested!
```

Fix: use `and_then` when the closure itself returns an `Option` or `Result`.

## Verification

```bash
# Exercise 1 -- Option basics
cargo run

# Exercise 2 -- Result and ? operator
cargo run

# Exercise 3 -- combinators
cargo run

# Exercise 4 -- config parser (the main challenge)
cargo run

# Exercise 5 -- conversions
cargo run
```

For each exercise, compare your predictions against the actual output. Pay special attention to exercises 2 and 4 where multiple error paths exist.

## Summary

- `Option<T>` replaces null: `Some(T)` or `None`, checked at compile time.
- `Result<T, E>` replaces exceptions: `Ok(T)` or `Err(E)`, also checked at compile time.
- `unwrap`/`expect` are for tests and prototypes, not production error handling.
- The `?` operator short-circuits on `None`/`Err` and keeps code flat.
- Combinators (`map`, `and_then`, `unwrap_or`) let you transform values without manual matching.
- `ok_or` and `ok()` convert between `Option` and `Result`.

## What's Next

Vectors -- Rust's growable array type. You will use `Option` and `Result` constantly when working with vectors (`.get()` returns `Option`, `.pop()` returns `Option`), so everything here feeds directly into the next exercise.

## Resources

- [The Rust Book -- Error Handling](https://doc.rust-lang.org/book/ch09-00-error-handling.html)
- [The Rust Book -- Option enum](https://doc.rust-lang.org/book/ch06-01-defining-an-enum.html#the-option-enum)
- [Rust By Example -- Error handling](https://doc.rust-lang.org/rust-by-example/error.html)
- [std::option::Option](https://doc.rust-lang.org/std/option/enum.Option.html)
- [std::result::Result](https://doc.rust-lang.org/std/result/enum.Result.html)
