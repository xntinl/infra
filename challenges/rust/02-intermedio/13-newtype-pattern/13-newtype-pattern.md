# 13. Newtype Pattern

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-basico exercises (structs, ownership, references)
- Completed: 01-traits, 02-generics, 11-type-conversions, 12-operator-overloading
- Familiar with `From`/`Into`, `Deref`, and trait implementations

## Learning Objectives

- Apply the newtype pattern to create type-safe wrappers that prevent value confusion
- Implement `Deref` and `From`/`Into` to make newtypes ergonomic without sacrificing safety
- Analyze the orphan rule and use newtypes to bypass it
- Evaluate the zero-cost abstraction guarantee of newtypes
- Implement validation newtypes that enforce invariants at construction time

## Concepts

### What Is the Newtype Pattern?

A newtype is a tuple struct with a single field:

```rust
struct Meters(f64);
struct Seconds(f64);
struct UserId(u64);
struct EmailAddress(String);
```

That is it. One field, wrapped in a new type. This pattern appears trivial, but it solves several important problems in Rust.

### Problem 1: Preventing Value Confusion

Consider a function that takes a distance and a duration:

```rust
fn calculate_speed(distance: f64, time: f64) -> f64 {
    distance / time
}

// Easy to mix up the arguments:
let speed = calculate_speed(time_in_seconds, distance_in_meters); // wrong!
```

The compiler cannot help because both parameters are `f64`. With newtypes:

```rust
struct Meters(f64);
struct Seconds(f64);
struct MetersPerSecond(f64);

fn calculate_speed(distance: Meters, time: Seconds) -> MetersPerSecond {
    MetersPerSecond(distance.0 / time.0)
}

// Compiler catches the mistake:
// calculate_speed(Seconds(10.0), Meters(100.0)); // ERROR: wrong types
```

This is not theoretical. The Mars Climate Orbiter was lost because one team used metric units and another used imperial, and there was no type system to catch the mismatch. Newtypes would have prevented it.

### Problem 2: The Orphan Rule

Rust's orphan rule says you can only implement a trait for a type if you own either the trait or the type. You cannot implement `Display` for `Vec<String>` because you own neither. But you can wrap `Vec<String>` in a newtype:

```rust
struct CommaSeparated(Vec<String>);

impl std::fmt::Display for CommaSeparated {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0.join(", "))
    }
}
```

Now you own `CommaSeparated`, so you can implement any trait on it.

### Problem 3: Type-Level Documentation

Even when there is no risk of confusion, newtypes make APIs self-documenting:

```rust
// What does this return? A user id? An order count? A database row?
fn process(input: &str) -> u64 { /* ... */ }

// Now it's clear:
fn process(input: &str) -> OrderId { /* ... */ }
```

### Making Newtypes Ergonomic: Deref

The main complaint about newtypes is that they are annoying to use. You have to write `.0` everywhere to access the inner value. `Deref` solves this for read access:

```rust
use std::ops::Deref;

struct Username(String);

impl Deref for Username {
    type Target = str;

    fn deref(&self) -> &str {
        &self.0
    }
}

let user = Username(String::from("alice"));
println!("Length: {}", user.len());      // deref coercion: &Username -> &str
println!("Upper: {}", user.to_uppercase()); // all str methods available
```

Important caveat: `Deref` should only be used when the newtype genuinely "is" the inner type with extra restrictions. A `Username` *is* a string (with validation). A `UserId` *is* a number (with meaning). Do not use `Deref` on types where the wrapper adds fundamentally different behavior.

### Making Newtypes Ergonomic: From/Into

`From` and `Into` let you convert between the newtype and its inner type:

```rust
struct Meters(f64);

impl From<f64> for Meters {
    fn from(val: f64) -> Self {
        Meters(val)
    }
}

impl From<Meters> for f64 {
    fn from(m: Meters) -> f64 {
        m.0
    }
}

let d: Meters = 42.0.into();
let raw: f64 = d.into();
```

But think carefully about the `From<f64>` direction. If your newtype has validation (e.g., must be positive), use `TryFrom` instead:

```rust
impl TryFrom<f64> for Meters {
    type Error = String;

    fn try_from(val: f64) -> Result<Self, String> {
        if val >= 0.0 {
            Ok(Meters(val))
        } else {
            Err(format!("distance cannot be negative: {val}"))
        }
    }
}
```

### Zero-Cost Abstraction

Newtypes have zero runtime overhead. The compiler eliminates the wrapper entirely. In the compiled binary, `Meters(42.0)` is represented exactly the same as `42.0_f64`. You get type safety at compile time and bare metal performance at runtime.

You can verify this with `#[repr(transparent)]`, which guarantees the newtype has the same memory layout as its inner field:

```rust
#[repr(transparent)]
struct Meters(f64);
```

### Validation Newtypes

The most powerful use of newtypes is enforcing invariants. If a value must satisfy some constraint (non-empty string, positive number, valid email), you can make it impossible to construct an invalid instance:

```rust
struct NonEmptyString(String);

impl NonEmptyString {
    pub fn new(s: impl Into<String>) -> Result<Self, &'static str> {
        let s = s.into();
        if s.is_empty() {
            Err("string must not be empty")
        } else {
            Ok(NonEmptyString(s))
        }
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}
```

The field is private, so no one outside the module can construct a `NonEmptyString` without going through `new()`. Once you have a `NonEmptyString`, you *know* it is not empty. No runtime checks needed at usage sites.

### Pattern vs Anti-Pattern

**Good use**: `UserId(u64)` where IDs should never be mixed with counts, ages, or other integers.

**Good use**: `HtmlEscaped(String)` where you need to track whether a string has been escaped.

**Anti-pattern**: `MyI32(i32)` with no purpose. If the wrapper adds no meaning or safety, it just adds noise.

**Anti-pattern**: Implementing `DerefMut` to allow mutation of the inner value on a validation newtype. If you validate on construction but allow arbitrary mutation, the invariant is broken.

## Exercises

### Exercise 1: Unit Safety

```rust
use std::fmt;

// TODO: Define these newtypes, all wrapping f64:
//   Kilometers, Miles, Meters

// TODO: Implement From<Kilometers> for Meters (1 km = 1000 m)
// TODO: Implement From<Miles> for Meters (1 mile = 1609.34 m)
// TODO: Implement From<Miles> for Kilometers (1 mile = 1.60934 km)

// TODO: Implement Display for each type:
//   Kilometers: "5.00 km"
//   Miles: "3.11 mi"
//   Meters: "5000.00 m"
// Use 2 decimal places by default.

// TODO: Write a function that computes travel time.
// It must be impossible to pass the arguments in the wrong order.
fn travel_time(distance: Kilometers, speed_kmh: KilometersPerHour) -> Hours {
    // TODO: Implement. Result = distance / speed.
    todo!()
}

// You'll also need:
// struct KilometersPerHour(f64);
// struct Hours(f64);
// with Display implementations.

fn main() {
    let marathon = Miles(26.2);
    let marathon_km: Kilometers = marathon.into();
    let marathon_m: Meters = marathon.into();

    println!("Marathon: {marathon}");
    println!("Marathon: {marathon_km}");
    println!("Marathon: {marathon_m}");

    let speed = KilometersPerHour(60.0);
    let time = travel_time(marathon_km, speed);
    println!("At {speed}, the marathon takes {time}");

    // This should NOT compile — uncomment to verify:
    // travel_time(speed, marathon_km); // ERROR: types swapped
}
```

### Exercise 2: Orphan Rule Bypass

```rust
use std::fmt;

// Problem: We want to display a Vec<String> as a comma-separated list.
// But we cannot implement Display for Vec<String> (orphan rule).

// TODO: Create a newtype `CommaSeparated` that wraps Vec<String>.

// TODO: Implement Display for CommaSeparated
// Output: "alice, bob, carol" (elements joined by ", ")
// Empty vec: "(empty)"

// TODO: Implement From<Vec<String>> for CommaSeparated
// TODO: Implement From<Vec<&str>> for CommaSeparated
// Hint: map each &str to String

// TODO: Implement Deref for CommaSeparated, targeting [String]
// This gives access to .len(), .iter(), .is_empty(), etc.

fn main() {
    let names: CommaSeparated = vec!["Alice", "Bob", "Carol"].into();
    println!("{names}");           // Alice, Bob, Carol
    println!("Count: {}", names.len()); // uses Deref to [String]

    let empty: CommaSeparated = Vec::<String>::new().into();
    println!("{empty}");           // (empty)

    // Deref lets us iterate without accessing .0:
    for name in names.iter() {
        println!("  - {name}");
    }
}
```

### Exercise 3: Validation Newtype

```rust
use std::fmt;

/// A non-empty, trimmed string that contains no whitespace-only content.
struct NonBlank(String);

// TODO: Implement NonBlank with a private inner field.
// The constructor should:
//   1. Trim the input
//   2. Reject empty or whitespace-only strings
//   3. Return Result<NonBlank, &'static str>

// TODO: Implement Deref<Target = str> for NonBlank
// TODO: Implement Display for NonBlank (just display the inner string)
// TODO: Implement Debug for NonBlank (show NonBlank("value"))

/// A valid port number (1-65535).
struct Port(u16);

// TODO: Implement Port with TryFrom<u16>
// Reject 0 (not a valid port).
// Also implement TryFrom<i32> for Port, rejecting negatives and values > 65535.
// TODO: Implement Display, Debug, and Copy+Clone for Port.

/// A percentage value between 0.0 and 100.0 inclusive.
struct Percentage(f64);

// TODO: Implement Percentage with TryFrom<f64>
// Reject NaN, negative values, and values above 100.
// TODO: Implement Display (format as "85.0%")

fn main() {
    // NonBlank:
    let name = NonBlank::new("  Alice  ");
    println!("{:?}", name);  // Ok(NonBlank("Alice"))

    let blank = NonBlank::new("   ");
    println!("{:?}", blank); // Err("string must not be blank")

    let name = name.unwrap();
    println!("Name: {name}");
    println!("Length: {}", name.len()); // Deref to str

    // Port:
    let port = Port::try_from(8080_u16);
    println!("{:?}", port);  // Ok(Port(8080))

    let bad_port = Port::try_from(0_u16);
    println!("{:?}", bad_port); // Err(...)

    let from_int = Port::try_from(443_i32);
    println!("{:?}", from_int); // Ok(Port(443))

    let negative = Port::try_from(-1_i32);
    println!("{:?}", negative); // Err(...)

    // Percentage:
    let p = Percentage::try_from(85.5);
    println!("{:?}", p);  // Ok(Percentage(85.5))
    if let Ok(pct) = p {
        println!("{pct}");  // 85.5%
    }

    let bad = Percentage::try_from(150.0);
    println!("{:?}", bad); // Err(...)

    let nan = Percentage::try_from(f64::NAN);
    println!("{:?}", nan); // Err(...)
}
```

### Exercise 4: Deref for Transparent Access

```rust
use std::ops::Deref;
use std::fmt;

/// A case-insensitive string wrapper.
/// Stores the original string but compares case-insensitively.
#[derive(Debug, Clone)]
struct CiString(String);

impl CiString {
    fn new(s: impl Into<String>) -> Self {
        CiString(s.into())
    }
}

// TODO: Implement Deref<Target = str> for CiString
// This gives access to all str methods (.len(), .contains(), .starts_with(), etc.)

// TODO: Implement Display for CiString (display the original casing)

// TODO: Implement PartialEq for CiString
// Compare case-insensitively: "Hello" == "hello"
// Hint: self.0.to_lowercase() == other.0.to_lowercase()

// TODO: Implement PartialEq<str> for CiString
// So you can write: ci_string == "hello"

// TODO: Implement PartialEq<CiString> for str
// So you can write: "hello" == ci_string

// TODO: Implement From<&str> for CiString
// TODO: Implement From<String> for CiString

impl Eq for CiString {}

fn main() {
    let a = CiString::new("Hello World");
    let b = CiString::new("hello world");
    let c = CiString::new("HELLO WORLD");

    // Case-insensitive comparison:
    assert_eq!(a, b);
    assert_eq!(b, c);
    assert_eq!(a, c);
    println!("All equal (case-insensitive): pass");

    // Cross-type comparison:
    assert_eq!(a, *"hello world");
    assert!(a == *"HELLO WORLD");
    println!("Cross-type comparison: pass");

    // Deref gives us str methods:
    println!("Length: {}", a.len());
    println!("Contains 'World': {}", a.contains("World"));
    println!("Uppercase: {}", a.to_uppercase());

    // Display shows original casing:
    println!("Display: {a}"); // Hello World

    // Works in collections:
    let mut set = std::collections::HashSet::new();
    set.insert(CiString::new("rust"));
    // This should not insert a duplicate:
    let inserted = set.insert(CiString::new("RUST"));
    // TODO: What will `inserted` be? Think about it before uncommenting.
    // println!("Inserted duplicate? {inserted}");
    // Hint: HashSet uses Eq + Hash. Did you implement Hash for CiString?
    // You need to implement Hash consistently with Eq for this to work.
}
```

To make the `HashSet` part work, you also need:

```rust
use std::hash::{Hash, Hasher};

// TODO: Implement Hash for CiString
// Hash the lowercase version so it's consistent with the case-insensitive Eq.
impl Hash for CiString {
    fn hash<H: Hasher>(&self, state: &mut H) {
        self.0.to_lowercase().hash(state);
    }
}
```

### Exercise 5: Zero-Cost Proof

```rust
/// This exercise demonstrates that newtypes have zero runtime cost.
/// We compare the size and behavior of raw values vs newtype wrappers.

#[repr(transparent)]
struct Meters(f64);

#[repr(transparent)]
struct UserId(u64);

#[repr(transparent)]
struct Name(String);

fn main() {
    // TODO: Use std::mem::size_of to print the size of each newtype
    // and its inner type. Verify they are identical.
    println!("f64:    {} bytes", std::mem::size_of::<f64>());
    println!("Meters: {} bytes", std::mem::size_of::<Meters>());
    // TODO: Do the same for u64/UserId and String/Name

    // TODO: Use std::mem::align_of to verify alignment is also identical.

    // Demonstrate that newtypes are truly zero-cost in a performance-sensitive
    // context. This loop should compile to identical assembly whether you
    // use f64 or Meters:
    fn sum_raw(values: &[f64]) -> f64 {
        values.iter().sum()
    }

    fn sum_meters(values: &[Meters]) -> f64 {
        // TODO: Implement this. Sum the inner values.
        // Hint: values.iter().map(|m| m.0).sum()
        todo!()
    }

    let raw = vec![1.0, 2.0, 3.0, 4.0, 5.0];
    let wrapped: Vec<Meters> = vec![
        Meters(1.0), Meters(2.0), Meters(3.0), Meters(4.0), Meters(5.0),
    ];

    println!("Raw sum: {}", sum_raw(&raw));
    println!("Wrapped sum: {}", sum_meters(&wrapped));
    // Both should print 15.0
}
```

## Try It Yourself

1. **Id type family**: Create a generic newtype `Id<T>(u64)` where `T` is a phantom type parameter. Define marker types `User`, `Order`, `Product`. Now `Id<User>` and `Id<Order>` are incompatible at compile time even though both wrap `u64`. Implement `Display`, `Debug`, `Clone`, `Copy`, `PartialEq`, `Eq`, and `Hash`.

2. **Sanitized HTML**: Create a `SafeHtml(String)` newtype that can only be constructed by escaping an input string. Implement a `from_raw(input: &str) -> SafeHtml` function that escapes `<`, `>`, `&`, and `"`. Then implement `Display` and verify that the escaped content is always safe.

3. **Bounded integer**: Create `BoundedU32<const MIN: u32, const MAX: u32>(u32)` using const generics. The constructor should reject values outside `[MIN, MAX]`. Test with `type Percentage = BoundedU32<0, 100>`.

4. **Currency newtype**: Create `Usd(i64)` and `Eur(i64)` where the inner value is cents. Implement `Add` for same-currency addition but make cross-currency addition a compile error. Implement `Display` to format as "$1.23" or "1.23 EUR".

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Public inner field on validation newtype | Anyone can construct invalid values | Make the field private, expose only validated constructors |
| `DerefMut` on validation newtype | Invariants can be violated after construction | Only implement `Deref`, not `DerefMut`, for validation types |
| Implementing `Deref` on non-wrapper types | Confusing method resolution, unexpected coercion | Reserve `Deref` for genuine smart-pointer/wrapper types |
| Forgetting `#[repr(transparent)]` | Usually fine, but not ABI-safe for FFI | Add `repr(transparent)` when the newtype crosses FFI boundaries |
| Implementing `From` instead of `TryFrom` for validated types | Bypasses validation | Use `TryFrom` when construction can fail |
| Inconsistent `Hash` and `Eq` implementations | Collections behave incorrectly | If `a == b`, then `hash(a) == hash(b)` must hold |

## Verification

- Exercise 1: Conversions produce correct values. Swapping arguments to `travel_time` causes a compile error.
- Exercise 2: `CommaSeparated` displays correctly. `Deref` gives slice methods.
- Exercise 3: Invalid inputs are rejected. Valid newtypes provide access via `Deref`.
- Exercise 4: Case-insensitive comparison works. `HashSet` treats `"rust"` and `"RUST"` as the same.
- Exercise 5: All sizes match between newtypes and their inner types.

## Summary

The newtype pattern wraps a single value in a struct to create a distinct type. It solves three problems: preventing accidental value confusion (Meters vs Seconds), bypassing the orphan rule (implementing foreign traits on foreign types), and enforcing invariants through validated constructors. With `Deref` and `From`/`Into`, newtypes remain ergonomic. With `#[repr(transparent)]`, they are guaranteed zero-cost. The pattern is simple in form but profound in impact -- it turns runtime bugs into compile-time errors.

## What's Next

- Exercise 14 covers the builder pattern, which often uses newtypes for required fields
- Exercise 15 introduces the state machine pattern, where types (including newtypes) represent states

## Resources

- [The Rust Book: Newtype Pattern](https://doc.rust-lang.org/book/ch19-04-advanced-types.html#using-the-newtype-pattern-for-type-safety-and-abstraction)
- [Rust Design Patterns: Newtype](https://rust-unofficial.github.io/patterns/patterns/behavioural/newtype.html)
- [Rust API Guidelines: Newtype](https://rust-lang.github.io/api-guidelines/type-safety.html)
- [repr(transparent)](https://doc.rust-lang.org/reference/type-layout.html#the-transparent-representation)
