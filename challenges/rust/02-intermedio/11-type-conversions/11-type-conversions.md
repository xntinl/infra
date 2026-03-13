# 11. Type Conversions

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-basico exercises (scalar types, ownership, references)
- Completed: 01-traits, 02-generics, 07-error-handling-patterns
- Familiar with `Result<T, E>`, trait implementations, and generic bounds

## Learning Objectives

- Apply `From`/`Into` to define lossless conversions between types
- Apply `TryFrom`/`TryInto` for fallible conversions that may fail at runtime
- Analyze the difference between `AsRef`/`AsMut` (cheap references) and `Borrow`/`ToOwned` (ownership semantics)
- Implement `Deref`/`DerefMut` to create smart-pointer-like wrappers
- Evaluate when to use `Cow<T>` to avoid unnecessary allocations
- Identify pitfalls of the `as` keyword for numeric casts

## Concepts

### Why So Many Conversion Traits?

Rust has no implicit type coercion (with one exception: deref coercion). When you want to convert a value from one type to another, you must be explicit. But "explicit" does not mean "verbose" -- the standard library provides a family of traits that make conversions ergonomic while keeping the type system honest.

Each trait serves a different purpose:

| Trait | Direction | Fallible? | Allocates? | Use case |
|---|---|---|---|---|
| `From`/`Into` | Value to value | No | Maybe | Lossless, infallible conversions |
| `TryFrom`/`TryInto` | Value to value | Yes | Maybe | Conversions that can fail |
| `AsRef`/`AsMut` | Reference to reference | No | No | Cheap borrows |
| `Deref`/`DerefMut` | Smart pointer to inner | No | No | Transparent wrapper access |
| `Borrow`/`ToOwned` | Reference/owned pair | No | On `to_owned()` | Hash/Eq-consistent borrowing |
| `Cow<T>` | Either borrowed or owned | No | Lazily | Avoid cloning when not needed |

### From and Into

`From` is the most common conversion trait. Implement `From<T> for U` and you get `Into<U> for T` for free:

```rust
struct Celsius(f64);
struct Fahrenheit(f64);

impl From<Celsius> for Fahrenheit {
    fn from(c: Celsius) -> Self {
        Fahrenheit(c.0 * 9.0 / 5.0 + 32.0)
    }
}

let boiling = Celsius(100.0);
let f: Fahrenheit = boiling.into();     // uses the blanket Into impl
let f2 = Fahrenheit::from(Celsius(0.0)); // uses From directly
```

Rule of thumb: always implement `From`, never implement `Into` manually. The blanket impl handles it.

`From` is also how the `?` operator converts errors. If your function returns `Result<T, MyError>` and you call something that returns `Result<T, io::Error>`, the `?` operator calls `MyError::from(io_error)` automatically -- provided you have `impl From<io::Error> for MyError`.

### TryFrom and TryInto

When a conversion can fail, use `TryFrom`. It returns a `Result`:

```rust
struct Percentage(u8);

impl TryFrom<i32> for Percentage {
    type Error = String;

    fn try_from(value: i32) -> Result<Self, Self::Error> {
        if (0..=100).contains(&value) {
            Ok(Percentage(value as u8))
        } else {
            Err(format!("{value} is not a valid percentage (0-100)"))
        }
    }
}

let valid: Result<Percentage, _> = 85.try_into();   // Ok
let invalid: Result<Percentage, _> = 150.try_into(); // Err
```

### AsRef and AsMut

These are for cheap reference-to-reference conversions. They never allocate. The classic example is functions that accept "anything string-like":

```rust
fn print_path(path: impl AsRef<std::path::Path>) {
    println!("{}", path.as_ref().display());
}

print_path("/tmp/file.txt");           // &str implements AsRef<Path>
print_path(String::from("/tmp/other")); // String implements AsRef<Path>
print_path(std::path::PathBuf::from("/tmp")); // PathBuf too
```

### Deref and DerefMut

`Deref` enables *deref coercion*: the compiler automatically calls `.deref()` when it needs to convert `&YourType` to `&InnerType`. This is the one place Rust has something resembling implicit conversion:

```rust
use std::ops::Deref;

struct MyString(String);

impl Deref for MyString {
    type Target = str;

    fn deref(&self) -> &str {
        &self.0
    }
}

let s = MyString(String::from("hello"));
// Deref coercion: &MyString -> &str automatically
println!("Length: {}", s.len()); // calls str::len()
```

Warning: do not implement `Deref` just for convenience on arbitrary types. It is meant for smart-pointer-like wrappers where the inner type is the "real" value. Misusing it creates confusing APIs.

### Borrow and ToOwned

`Borrow<T>` is like `AsRef<T>` but with a stronger contract: the borrowed and owned forms must have identical `Hash`, `Eq`, and `Ord` behavior. This is what lets `HashMap<String, V>` accept `&str` keys for lookups:

```rust
use std::collections::HashMap;

let mut map = HashMap::new();
map.insert(String::from("key"), 42);

// This works because String: Borrow<str>
// and Hash/Eq are consistent between String and str
let val = map.get("key"); // passing &str, not &String
```

`ToOwned` is the inverse -- it creates an owned value from a borrowed one. `str::to_owned()` returns a `String`, `[T]::to_owned()` returns a `Vec<T>`.

### Cow<T>: Clone on Write

`Cow<'a, T>` holds either a `&'a T` (borrowed) or a `T::Owned` (owned). It defers cloning until mutation is actually needed:

```rust
use std::borrow::Cow;

fn maybe_uppercase(input: &str, shout: bool) -> Cow<'_, str> {
    if shout {
        Cow::Owned(input.to_uppercase()) // allocates only when needed
    } else {
        Cow::Borrowed(input) // zero-cost: just a reference
    }
}

let quiet = maybe_uppercase("hello", false);  // no allocation
let loud = maybe_uppercase("hello", true);    // allocates
println!("{quiet}, {loud}");
```

This is valuable in parsers and transformations where most inputs pass through unchanged and only some need modification.

### The `as` Keyword: Handle with Care

`as` performs primitive casts. It is fast but dangerous because it silently truncates or wraps:

```rust
let big: i64 = 300;
let small: i8 = big as i8; // silently wraps to 44 (300 % 256 = 44)

let negative: i32 = -1;
let unsigned: u32 = negative as u32; // becomes 4294967295 (wrapping)

let float: f64 = 1e20;
let int: i32 = float as i32; // saturates to i32::MAX (2147483647)
```

Prefer `TryFrom`/`TryInto` for numeric conversions where loss is possible. Use `as` only when you are certain the conversion is safe, or when doing low-level work where wrapping is intentional.

## Exercises

### Exercise 1: From and Into

```rust
#[derive(Debug, PartialEq)]
struct Meters(f64);

#[derive(Debug, PartialEq)]
struct Kilometers(f64);

#[derive(Debug, PartialEq)]
struct Miles(f64);

// TODO: Implement From<Kilometers> for Meters
// 1 km = 1000 m

// TODO: Implement From<Miles> for Kilometers
// 1 mile = 1.60934 km

// TODO: Implement From<Miles> for Meters
// Chain the conversions: miles -> km -> meters
// Hint: you can call Kilometers::from(miles) inside the implementation

fn main() {
    let marathon = Miles(26.2);

    let km = Kilometers::from(Miles(26.2));
    println!("{:?}", km); // Kilometers(42.164708)

    // Using Into (the free blanket impl)
    let m: Meters = marathon.into();
    println!("{:?}", m); // Meters(42164.708...)

    // From also enables this pattern for function arguments:
    fn print_distance(d: impl Into<Meters>) {
        let meters = d.into();
        println!("Distance: {:?}", meters);
    }

    print_distance(Kilometers(5.0));
    print_distance(Miles(3.1));
}
```

### Exercise 2: TryFrom for Validation

```rust
#[derive(Debug)]
struct Email {
    local: String,
    domain: String,
}

#[derive(Debug)]
enum EmailError {
    MissingAtSign,
    EmptyLocal,
    EmptyDomain,
    MultiplAtSigns,
}

// TODO: Implement std::fmt::Display for EmailError
// Each variant should produce a human-readable message.

// TODO: Implement TryFrom<&str> for Email
// Validation rules:
//   1. Must contain exactly one '@'
//   2. The part before '@' (local) must not be empty
//   3. The part after '@' (domain) must not be empty
// On success, return Email { local, domain }

// TODO: Implement std::fmt::Display for Email
// Format as "{local}@{domain}"

fn main() {
    let valid: Result<Email, _> = "user@example.com".try_into();
    let no_at: Result<Email, _> = "userexample.com".try_into();
    let empty_local: Result<Email, _> = "@example.com".try_into();
    let double_at: Result<Email, _> = "user@@example.com".try_into();

    println!("Valid: {:?}", valid);
    println!("No @: {:?}", no_at);
    println!("Empty local: {:?}", empty_local);
    println!("Double @: {:?}", double_at);

    // TryFrom also works well with the ? operator:
    fn send_email(addr: &str) -> Result<(), EmailError> {
        let email = Email::try_from(addr)?;
        println!("Sending to {email}");
        Ok(())
    }

    let _ = send_email("alice@example.com");
    let _ = send_email("broken");
}
```

### Exercise 3: AsRef for Flexible APIs

```rust
use std::path::Path;

struct Config {
    base_dir: String,
}

impl Config {
    fn new(base_dir: impl AsRef<str>) -> Self {
        Config {
            base_dir: base_dir.as_ref().to_string(),
        }
    }

    // TODO: Implement a method `resolve_path` that takes anything implementing
    // AsRef<Path> and returns a full path by joining base_dir with the argument.
    // Return type: std::path::PathBuf
    // Hint: Path::new(&self.base_dir).join(relative)

    // TODO: Implement a method `read_setting` that takes a key as
    // impl AsRef<str> and returns a dummy string.
    // The point is to accept &str, String, and &String seamlessly.
}

fn main() {
    let config = Config::new("/etc/myapp");

    // All of these should work thanks to AsRef:
    let p1 = config.resolve_path("config.toml");
    let p2 = config.resolve_path(String::from("data/db.sqlite"));
    let p3 = config.resolve_path(Path::new("logs/app.log"));

    println!("{}", p1.display());
    println!("{}", p2.display());
    println!("{}", p3.display());

    let key = String::from("database_url");
    config.read_setting("timeout");
    config.read_setting(&key);
    config.read_setting(key);
}
```

### Exercise 4: Cow for Conditional Allocation

```rust
use std::borrow::Cow;

/// Normalizes a username:
/// - Trims whitespace
/// - Lowercases
/// - If the input was already trimmed and lowercase, return a borrow (no allocation)
/// - Otherwise, return an owned String
fn normalize_username(input: &str) -> Cow<'_, str> {
    // TODO: Implement this function.
    // Step 1: Check if the input is already normalized
    //         (no leading/trailing whitespace, all lowercase)
    // Step 2: If yes, return Cow::Borrowed(input)
    // Step 3: If no, return Cow::Owned(input.trim().to_lowercase())
    todo!()
}

/// Escapes HTML special characters in a string.
/// If the input contains no special characters, returns a borrow.
/// Otherwise, allocates a new string with replacements.
fn escape_html(input: &str) -> Cow<'_, str> {
    // TODO: Implement this function.
    // Characters to escape: & -> &amp;  < -> &lt;  > -> &gt;  " -> &quot;
    // Hint: first check if any special chars exist with .contains().
    // If none exist, return Cow::Borrowed(input).
    // If they do, iterate and build a new String.
    todo!()
}

fn main() {
    // These should NOT allocate (already normalized):
    let a = normalize_username("alice");
    let b = normalize_username("bob");

    // These MUST allocate (need transformation):
    let c = normalize_username("  Alice  ");
    let d = normalize_username("BOB");

    // You can check which variant you got:
    println!("a borrowed? {}", matches!(a, Cow::Borrowed(_)));
    println!("c borrowed? {}", matches!(c, Cow::Borrowed(_)));

    // Cow<str> implements Display and Deref<Target=str>:
    println!("a = {a}, c = {c}");

    // HTML escaping:
    let safe = escape_html("Hello, world!");
    let unsafe_input = escape_html("<script>alert('xss')</script>");
    println!("safe borrowed? {}", matches!(safe, Cow::Borrowed(_)));
    println!("escaped: {unsafe_input}");
}
```

### Exercise 5: The `as` Trap and Safe Alternatives

```rust
fn main() {
    // --- Part A: Predict the output ---
    // For each cast, predict the value BEFORE running the code.
    // Write your prediction as a comment, then verify.

    let a: u8 = 255_u16 as u8;
    println!("a = {a}");  // prediction: ___

    let b: u8 = 256_u16 as u8;
    println!("b = {b}");  // prediction: ___

    let c: i8 = 128_u8 as i8;
    println!("c = {c}");  // prediction: ___

    let d: u32 = -1_i32 as u32;
    println!("d = {d}");  // prediction: ___

    let e: i32 = 3.99_f64 as i32;
    println!("e = {e}");  // prediction: ___

    let f: u8 = 1000_f32 as u8;
    println!("f = {f}");  // prediction: ___

    // --- Part B: Safe conversions ---
    // TODO: Rewrite each of the dangerous casts above using TryFrom/TryInto.
    // Print Ok(value) or Err(message) for each.

    let safe_a: Result<u8, _> = u8::try_from(255_u16);
    println!("safe_a = {safe_a:?}");

    // TODO: Do the same for b, c, d
    // Note: there is no TryFrom<f64> for i32 in std.
    // For float-to-int, you can check the range manually:
    fn safe_f64_to_i32(val: f64) -> Result<i32, String> {
        if val >= i32::MIN as f64 && val <= i32::MAX as f64 {
            Ok(val as i32)
        } else {
            Err(format!("{val} out of i32 range"))
        }
    }

    println!("safe_e = {:?}", safe_f64_to_i32(3.99));
    println!("safe_f = {:?}", safe_f64_to_i32(1e20));
}
```

## Try It Yourself

1. **Conversion chain**: Create types `Seconds`, `Minutes`, and `Hours`. Implement `From` conversions so you can go from `Hours` to `Minutes` to `Seconds`. Then write a function that accepts `impl Into<Seconds>` and prints the value.

2. **TryFrom with enums**: Create an enum `HttpStatus` with variants `Ok`, `NotFound`, `ServerError`. Implement `TryFrom<u16>` that maps 200, 404, 500 to the corresponding variants and rejects everything else.

3. **Cow in a parser**: Write a function `strip_comments(line: &str) -> Cow<'_, str>` that removes everything after `//`. If the line has no comment, return it as a borrow. If it does, return an owned trimmed version.

4. **AsRef chain**: Write a function that accepts `impl AsRef<[u8]>` and computes a simple checksum (sum of all bytes modulo 256). Verify it works with `&[u8]`, `Vec<u8>`, and `&str`.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Implementing `Into` manually | Compiler warning or confusion | Implement `From` instead; `Into` comes for free |
| Using `as` for user input conversion | Silent data corruption | Use `TryFrom`/`TryInto` and handle errors |
| Implementing `Deref` for non-wrapper types | Confusing API, unexpected method resolution | Only use `Deref` for smart-pointer patterns |
| Returning `Cow::Owned` always | Defeats the purpose, always allocates | Check if the input is already in the desired form |
| Forgetting `type Error` in `TryFrom` | "missing associated type" error | Always define the error type |
| `AsRef` vs `Into` confusion | Borrow vs ownership mismatch | `AsRef` borrows cheaply, `Into` takes ownership |

## Verification

- Exercise 1: All distance conversions print correct values. `print_distance` accepts both `Kilometers` and `Miles`.
- Exercise 2: Valid email succeeds, invalid ones produce descriptive errors.
- Exercise 3: `resolve_path` and `read_setting` accept `&str`, `String`, and `Path`/`PathBuf`.
- Exercise 4: Normalized inputs produce `Cow::Borrowed`, un-normalized ones produce `Cow::Owned`.
- Exercise 5 Part A: Your predictions match the actual output (watch for wrapping and truncation). Part B: `TryFrom` catches the overflow cases.

## Summary

Rust's conversion traits form a layered system. `From`/`Into` handle infallible value-to-value conversions and power the `?` operator's error conversion. `TryFrom`/`TryInto` add fallibility for conversions that can fail. `AsRef`/`AsMut` provide cheap reference conversions for flexible function signatures. `Deref` enables transparent wrappers with automatic coercion. `Borrow`/`ToOwned` maintain Hash/Eq consistency for collection lookups. `Cow` defers allocation until mutation is needed. And `as` exists for primitive casts but should be treated with suspicion whenever the conversion might lose data.

## What's Next

- Exercise 12 covers operator overloading, which uses many of these same traits (especially `From` and `PartialEq`) in practice
- Exercise 13 introduces the newtype pattern, which relies heavily on `From`, `Into`, and `Deref` to remain ergonomic

## Resources

- [The Rust Book: Type Conversions](https://doc.rust-lang.org/book/ch09-02-recoverable-errors-with-result.html)
- [std::convert module](https://doc.rust-lang.org/std/convert/index.html)
- [Rust by Example: Conversion](https://doc.rust-lang.org/rust-by-example/conversion.html)
- [Cow documentation](https://doc.rust-lang.org/std/borrow/enum.Cow.html)
- [The Rustonomicon: Casts](https://doc.rust-lang.org/nomicon/casts.html)
