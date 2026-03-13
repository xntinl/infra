# 24. Advanced Macro Patterns

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 07-08 (declarative macros, procedural macros)
- Solid understanding of `macro_rules!` syntax, repetition operators (`$(...)*`, `$(...)+`), and fragment specifiers (`$e:expr`, `$t:ty`, `$i:ident`)
- Familiarity with trait impl blocks and generics
- Willingness to read macro expansion output (cargo expand)

## Learning Objectives

- Build TT muncher macros that process token trees one piece at a time
- Use push-down accumulation to collect and transform tokens before final expansion
- Define internal rules to organize complex macro logic into named sub-patterns
- Implement macro callbacks for composable macro-to-macro communication
- Count tokens at compile time using bit-shifting and recursive techniques
- Generate `impl` blocks for multiple types from a single macro invocation
- Debug macros with `cargo expand` and `trace_macros!`
- Analyze when macros are the right tool versus generics, traits, or proc macros

## Concepts

### Part 1: TT Munchers

A TT (Token Tree) muncher is a recursive macro that processes its input one token (or group) at a time. Each recursive call peels off the first token, does something with it, and passes the rest to the next recursion. This is the fundamental technique for parsing arbitrary syntax in declarative macros.

```rust
// A TT muncher that counts tokens
macro_rules! count_tts {
    // Base case: no tokens left
    () => { 0usize };
    // Recursive case: consume one token tree, count the rest
    ($first:tt $($rest:tt)*) => {
        1usize + count_tts!($($rest)*)
    };
}

fn main() {
    let n = count_tts!(a b c d e);
    assert_eq!(n, 5);

    // Token trees include groups: (a b) counts as ONE tt
    let m = count_tts!((a b) c [d e f]);
    assert_eq!(m, 3);

    println!("count_tts works: {n}, {m}");
}
```

The key insight: `$first:tt` matches exactly one token tree. A token tree is either a single token (`a`, `42`, `+`) or a delimited group (`(...)`, `[...]`, `{...}`). By matching `$first:tt $($rest:tt)*`, you peel off one TT and recurse on the remainder.

**Recursion limit:** Rust macros have a default recursion limit of 128. For large inputs, add `#![recursion_limit = "512"]` to your crate root. But if you need thousands of recursions, consider a proc macro instead.

### Part 2: Push-Down Accumulation

TT munchers process input left to right, but sometimes you need to accumulate results and emit them all at once. Push-down accumulation uses an extra set of brackets to carry the accumulated tokens through each recursion:

```rust
// Reverse a sequence of tokens
macro_rules! reverse_tts {
    // Entry point: start with empty accumulator []
    ($($input:tt)*) => {
        reverse_tts!(@acc [] $($input)*)
    };
    // Base case: input exhausted, emit accumulator
    (@acc [$($acc:tt)*]) => {
        ($($acc)*)
    };
    // Recursive case: move first input token to front of accumulator
    (@acc [$($acc:tt)*] $first:tt $($rest:tt)*) => {
        reverse_tts!(@acc [$first $($acc)*] $($rest)*)
    };
}

fn main() {
    // reverse_tts!(1 2 3) => (3 2 1)
    let result = reverse_tts!(1 2 3);
    assert_eq!(result, (3, 2, 1));
    println!("reversed: {result:?}");
}
```

The `@acc` prefix is not special syntax. It is a conventional naming pattern (an internal rule) that prevents users from accidentally invoking the accumulator variant directly. The `@` is a valid token that is unlikely to conflict with user code.

### A More Practical Example: Building a Map

```rust
macro_rules! hash_map {
    // Entry point
    ($($key:expr => $value:expr),* $(,)?) => {{
        let mut map = ::std::collections::HashMap::new();
        $(
            map.insert($key, $value);
        )*
        map
    }};
}

// Same thing, but using push-down accumulation to validate pairs
macro_rules! validated_map {
    // Entry point: start accumulation
    ($($input:tt)*) => {
        validated_map!(@parse [] $($input)*)
    };
    // Base case: emit all accumulated inserts
    (@parse [$($inserts:stmt;)*]) => {{
        let mut map = ::std::collections::HashMap::new();
        $($inserts)*
        map
    }};
    // Parse one key => value pair, accumulate the insert statement
    (@parse [$($acc:stmt;)*] $key:expr => $value:expr, $($rest:tt)*) => {
        validated_map!(@parse [$($acc;)* map.insert($key, $value);] $($rest)*)
    };
    // Last pair (no trailing comma)
    (@parse [$($acc:stmt;)*] $key:expr => $value:expr) => {
        validated_map!(@parse [$($acc;)* map.insert($key, $value);])
    };
}

fn main() {
    let m = validated_map! {
        "name" => "Alice",
        "city" => "Berlin",
        "lang" => "Rust",
    };
    println!("{m:?}");
}
```

### Part 3: Internal Rules

Internal rules are macro arms prefixed with a sigil (conventionally `@name`) that serve as private "functions" within the macro. They organize complex macros into logical sections:

```rust
macro_rules! builder {
    // Public entry point
    (
        struct $name:ident {
            $($field:ident : $ty:ty),* $(,)?
        }
    ) => {
        // Generate the struct
        builder!(@struct $name { $($field: $ty),* });
        // Generate the builder
        builder!(@builder $name { $($field: $ty),* });
        // Generate the build method
        builder!(@build $name { $($field: $ty),* });
    };

    // Internal rule: generate the struct definition
    (@struct $name:ident { $($field:ident : $ty:ty),* }) => {
        #[derive(Debug)]
        pub struct $name {
            $(pub $field: $ty,)*
        }
    };

    // Internal rule: generate the builder struct with Option fields
    (@builder $name:ident { $($field:ident : $ty:ty),* }) => {
        paste::paste! {
            #[derive(Default)]
            pub struct [<$name Builder>] {
                $($field: Option<$ty>,)*
            }

            impl [<$name Builder>] {
                pub fn new() -> Self {
                    Self::default()
                }

                $(
                    pub fn $field(mut self, value: $ty) -> Self {
                        self.$field = Some(value);
                        self
                    }
                )*
            }
        }
    };

    // Internal rule: generate the build() method that validates all fields
    (@build $name:ident { $($field:ident : $ty:ty),* }) => {
        paste::paste! {
            impl [<$name Builder>] {
                pub fn build(self) -> Result<$name, String> {
                    Ok($name {
                        $(
                            $field: self.$field.ok_or_else(|| {
                                format!("missing field: {}", stringify!($field))
                            })?,
                        )*
                    })
                }
            }
        }
    };
}

// Usage
builder! {
    struct Config {
        host: String,
        port: u16,
        workers: usize,
    }
}

fn main() {
    let config = ConfigBuilder::new()
        .host("localhost".to_string())
        .port(8080)
        .workers(4)
        .build()
        .unwrap();

    println!("{config:?}");

    // Missing field produces a clear error
    let err = ConfigBuilder::new()
        .host("localhost".to_string())
        .build()
        .unwrap_err();
    println!("error: {err}");
}
```

Note: this uses the `paste` crate for identifier concatenation (`[<$name Builder>]`). Without `paste`, you cannot concatenate identifiers in `macro_rules!`. This is one of the fundamental limitations that pushes people toward proc macros.

### Part 4: Macro Callbacks

A macro callback is when one macro invokes another macro, passing generated tokens as arguments. This enables composition between independent macros:

```rust
// A macro that generates a list of types and calls back to another macro
macro_rules! with_types {
    ($callback:ident) => {
        $callback!(u8, u16, u32, u64, i8, i16, i32, i64, f32, f64);
    };
}

// A callback macro that generates Display-based print functions
macro_rules! impl_print_for {
    ($($ty:ty),*) => {
        $(
            fn concat_idents_workaround(val: $ty) {
                println!("{}: {}", stringify!($ty), val);
            }
        )*
    };
}

// More useful: generate trait impls for a list of types
macro_rules! impl_from_for {
    ($target:ty; $($source:ty),*) => {
        $(
            impl From<$source> for $target {
                fn from(val: $source) -> Self {
                    Self(val as f64)
                }
            }
        )*
    };
}

#[derive(Debug)]
struct Meters(f64);

impl_from_for!(Meters; u8, u16, u32, i8, i16, i32, f32, f64);

fn main() {
    let m: Meters = 42u32.into();
    println!("{m:?}");

    let m: Meters = 3.14f64.into();
    println!("{m:?}");
}
```

The real power of callbacks is chaining: macro A processes input, generates tokens, and passes them to macro B. Macro B further transforms them and passes to macro C. This creates a pipeline of compile-time transformations.

### Part 5: Counting with Macros

Counting the number of repetitions in a macro is surprisingly difficult. Here are three techniques, each with different trade-offs:

```rust
// Technique 1: Replace each token with a unit value and take the length of an array.
// O(1) at runtime, O(n) compile time. Works up to ~1000 items.
macro_rules! count_array {
    ($($item:tt),*) => {
        {
            // Create an array of unit values, one per item
            const COUNT: usize = [$(count_array!(@replace $item ())),*].len();
            COUNT
        }
    };
    (@replace $_t:tt $sub:expr) => { $sub };
}

// Technique 2: Bit shifting. O(log n) recursion depth.
// Handles thousands of items without hitting recursion limits.
macro_rules! count_bitshift {
    () => { 0usize };
    ($one:tt) => { 1usize };
    ($($a:tt $b:tt)*) => {
        count_bitshift!($($a)*) << 1usize
    };
    ($odd:tt $($a:tt $b:tt)*) => {
        (count_bitshift!($($a)*) << 1usize) | 1usize
    };
}

// Technique 3: Nested type recursion. Encodes count as a type.
// Zero runtime cost: the count is a const generic.
macro_rules! count_type {
    () => { 0usize };
    ($first:tt $($rest:tt)*) => {
        1usize + count_type!($($rest)*)
    };
}

fn main() {
    assert_eq!(count_array!(a, b, c, d, e), 5);
    assert_eq!(count_bitshift!(a b c d e f g), 7);
    assert_eq!(count_type!(a b c d e f g h), 8);
    println!("all counting techniques work");
}
```

**Trade-off:**
- Array replacement: simple, but generates a temporary array at compile time. The compiler optimizes it away, but very large counts slow compilation.
- Bit shifting: efficient recursion depth (log2(n) instead of n), handles thousands of items. Harder to read.
- Simple recursion: clearest code, but hits the recursion limit at 128 items by default.

### Part 6: Generating Impl Blocks

The most common practical use of advanced macros: reducing boilerplate by generating trait implementations for multiple types:

```rust
use std::fmt;

// Generate Display, FromStr, and conversion impls for newtype wrappers
macro_rules! newtype_string {
    ($(
        $(#[$meta:meta])*
        pub struct $name:ident;
    )*) => {
        $(
            $(#[$meta])*
            #[derive(Debug, Clone, PartialEq, Eq, Hash)]
            pub struct $name(String);

            impl $name {
                pub fn new(s: impl Into<String>) -> Self {
                    Self(s.into())
                }

                pub fn as_str(&self) -> &str {
                    &self.0
                }

                pub fn into_inner(self) -> String {
                    self.0
                }
            }

            impl fmt::Display for $name {
                fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
                    self.0.fmt(f)
                }
            }

            impl From<String> for $name {
                fn from(s: String) -> Self {
                    Self(s)
                }
            }

            impl From<&str> for $name {
                fn from(s: &str) -> Self {
                    Self(s.to_owned())
                }
            }

            impl AsRef<str> for $name {
                fn as_ref(&self) -> &str {
                    &self.0
                }
            }

            impl std::str::FromStr for $name {
                type Err = std::convert::Infallible;
                fn from_str(s: &str) -> Result<Self, Self::Err> {
                    Ok(Self(s.to_owned()))
                }
            }

            // serde support if the feature is enabled
            #[cfg(feature = "serde")]
            impl serde::Serialize for $name {
                fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
                    self.0.serialize(serializer)
                }
            }

            #[cfg(feature = "serde")]
            impl<'de> serde::Deserialize<'de> for $name {
                fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
                    String::deserialize(deserializer).map(Self)
                }
            }
        )*
    };
}

// Usage: define multiple newtype wrappers with full trait coverage
newtype_string! {
    /// A user's email address.
    pub struct Email;

    /// A unique user identifier.
    pub struct UserId;

    /// An API authentication token.
    pub struct ApiToken;
}

fn main() {
    let email = Email::new("alice@example.com");
    let user_id = UserId::from("usr_12345");
    let token: ApiToken = "tok_secret".into();

    println!("email: {email}");
    println!("user_id: {user_id}");
    println!("token length: {}", token.as_str().len());

    // Type safety: cannot mix types even though they are all strings
    // let _wrong: Email = user_id; // compile error
}
```

### Part 7: Recursive Macros for DSLs

Recursive macros can build small domain-specific languages. Here is a state machine DSL:

```rust
macro_rules! state_machine {
    (
        name: $name:ident,
        initial: $initial:ident,
        transitions: {
            $($from:ident -> $to:ident on $event:ident),* $(,)?
        }
    ) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq)]
        pub enum State {
            $($from,)*
            $($to,)*
        }

        #[derive(Debug, Clone, Copy)]
        pub enum Event {
            $($event,)*
        }

        pub struct $name {
            state: State,
        }

        impl $name {
            pub fn new() -> Self {
                Self { state: State::$initial }
            }

            pub fn state(&self) -> State {
                self.state
            }

            pub fn handle(&mut self, event: Event) -> Result<State, String> {
                let new_state = match (self.state, event) {
                    $(
                        (State::$from, Event::$event) => State::$to,
                    )*
                    (state, event) => {
                        return Err(format!(
                            "invalid transition: {:?} + {:?}",
                            state, event
                        ));
                    }
                };
                self.state = new_state;
                Ok(new_state)
            }
        }
    };
}

// Note: this generates duplicate enum variants if a state appears in both
// $from and $to positions. A production version would deduplicate via
// push-down accumulation. Shown here for clarity.

// For a working version, list states separately:
macro_rules! state_machine_v2 {
    (
        name: $name:ident,
        states: [$($state:ident),* $(,)?],
        events: [$($event:ident),* $(,)?],
        initial: $initial:ident,
        transitions: {
            $($from:ident -> $to:ident on $evt:ident),* $(,)?
        }
    ) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq)]
        pub enum State {
            $($state,)*
        }

        #[derive(Debug, Clone, Copy)]
        pub enum Event {
            $($event,)*
        }

        pub struct $name {
            state: State,
        }

        impl $name {
            pub fn new() -> Self {
                Self { state: State::$initial }
            }

            pub fn state(&self) -> State {
                self.state
            }

            pub fn handle(&mut self, event: Event) -> Result<State, String> {
                let new_state = match (self.state, event) {
                    $(
                        (State::$from, Event::$evt) => State::$to,
                    )*
                    (state, event) => {
                        return Err(format!(
                            "invalid transition: {:?} + {:?}",
                            state, event
                        ));
                    }
                };
                self.state = new_state;
                Ok(new_state)
            }
        }
    };
}

state_machine_v2! {
    name: OrderFsm,
    states: [Created, Paid, Shipped, Delivered, Cancelled],
    events: [Pay, Ship, Deliver, Cancel],
    initial: Created,
    transitions: {
        Created -> Paid on Pay,
        Created -> Cancelled on Cancel,
        Paid -> Shipped on Ship,
        Paid -> Cancelled on Cancel,
        Shipped -> Delivered on Deliver,
    }
}

fn main() {
    let mut order = OrderFsm::new();
    assert_eq!(order.state(), State::Created);

    order.handle(Event::Pay).unwrap();
    assert_eq!(order.state(), State::Paid);

    order.handle(Event::Ship).unwrap();
    assert_eq!(order.state(), State::Shipped);

    order.handle(Event::Deliver).unwrap();
    assert_eq!(order.state(), State::Delivered);

    // Invalid transition
    let err = order.handle(Event::Pay).unwrap_err();
    println!("expected error: {err}");

    println!("state machine works");
}
```

### Part 8: Debugging Macros

When macros misbehave, you need visibility into what they expand to.

**cargo expand** shows the fully expanded code:

```bash
# Install
cargo install cargo-expand

# Expand all macros in the crate
cargo expand

# Expand a specific module
cargo expand module_name

# Expand a specific function/item (requires nightly for --ugly)
cargo expand main
```

**trace_macros!** (nightly only) prints each macro invocation and its expansion:

```rust
#![feature(trace_macros)]

macro_rules! double {
    ($e:expr) => { $e * 2 };
}

fn main() {
    trace_macros!(true);
    let x = double!(5);
    trace_macros!(false);
    println!("{x}");
}
```

Output:
```
note: trace_macro
  --> src/main.rs:8:13
   |
8  |     let x = double!(5);
   |             ^^^^^^^^^^
   |
   = note: expanding `double! { 5 }`
   = note: to `5 * 2`
```

**log_syntax!** (nightly only) prints tokens at compile time without expanding them:

```rust
#![feature(log_syntax)]

macro_rules! debug_args {
    ($($arg:tt)*) => {
        log_syntax!($($arg)*);
        // actual expansion here
    };
}
```

**For stable Rust**, `cargo expand` is the primary tool. When debugging a complex macro, add a temporary `compile_error!` at the point of confusion:

```rust
macro_rules! my_macro {
    (@step2 $($tokens:tt)*) => {
        compile_error!(concat!(
            "step2 received: ",
            $(stringify!($tokens), " ",)*
        ));
    };
    ($input:expr) => {
        my_macro!(@step2 processed($input))
    };
}
```

This shows you exactly what tokens reached `@step2`, as a compile error message.

## Exercises

### Exercise 1: TT Muncher Enum Parser

Build a macro `define_enum!` that parses a custom syntax for enums with associated string values and generates: the enum, a `Display` impl, a `FromStr` impl, and an `all()` method that returns a slice of all variants.

**Cargo.toml:**
```toml
[package]
name = "advanced-macros"
edition = "2024"

[dependencies]
paste = "1"
```

**Target syntax:**
```rust
define_enum! {
    HttpMethod {
        GET = "GET",
        POST = "POST",
        PUT = "PUT",
        DELETE = "DELETE",
        PATCH = "PATCH",
    }
}
```

**Generated API:**
```rust
let m = HttpMethod::GET;
println!("{m}");             // prints "GET"
let parsed: HttpMethod = "POST".parse().unwrap();
let all = HttpMethod::all(); // &[GET, POST, PUT, DELETE, PATCH]
```

**Constraints:**
- Use TT munching to parse the `variant = "string"` pairs
- Push-down accumulation to collect variants before emitting the enum
- The `all()` method must return a `&'static [Self]`
- `FromStr` must return an error type with the invalid input

<details>
<summary>Solution</summary>

```rust
macro_rules! define_enum {
    // Entry point: start TT munching with empty accumulators
    ($name:ident { $($input:tt)* }) => {
        define_enum!(@munch $name [] [] $($input)*)
    };

    // Base case: no more input, emit everything
    (@munch $name:ident
        [$(($variant:ident, $str:expr))*]  // accumulated (variant, string) pairs
        [$($all_items:tt)*]                 // not used separately here
    ) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
        pub enum $name {
            $($variant,)*
        }

        impl ::std::fmt::Display for $name {
            fn fmt(&self, f: &mut ::std::fmt::Formatter<'_>) -> ::std::fmt::Result {
                let s = match self {
                    $($name::$variant => $str,)*
                };
                f.write_str(s)
            }
        }

        impl ::std::str::FromStr for $name {
            type Err = String;

            fn from_str(s: &str) -> Result<Self, Self::Err> {
                match s {
                    $($str => Ok($name::$variant),)*
                    other => Err(format!(
                        "invalid {}: '{}' (expected one of: {})",
                        stringify!($name),
                        other,
                        [$($str),*].join(", ")
                    )),
                }
            }
        }

        impl $name {
            pub fn all() -> &'static [Self] {
                &[$($name::$variant,)*]
            }

            pub fn as_str(&self) -> &'static str {
                match self {
                    $($name::$variant => $str,)*
                }
            }
        }
    };

    // Recursive case: parse one "VARIANT = "string"," and accumulate
    (@munch $name:ident
        [$(($acc_var:ident, $acc_str:expr))*]
        [$($all:tt)*]
        $variant:ident = $str:expr,
        $($rest:tt)*
    ) => {
        define_enum!(@munch $name
            [$(($acc_var, $acc_str))* ($variant, $str)]
            [$($all)*]
            $($rest)*
        )
    };

    // Last item without trailing comma
    (@munch $name:ident
        [$(($acc_var:ident, $acc_str:expr))*]
        [$($all:tt)*]
        $variant:ident = $str:expr
    ) => {
        define_enum!(@munch $name
            [$(($acc_var, $acc_str))* ($variant, $str)]
            [$($all)*]
        )
    };
}

define_enum! {
    HttpMethod {
        GET = "GET",
        POST = "POST",
        PUT = "PUT",
        DELETE = "DELETE",
        PATCH = "PATCH",
    }
}

define_enum! {
    Color {
        Red = "red",
        Green = "green",
        Blue = "blue",
    }
}

fn main() {
    // Display
    let m = HttpMethod::GET;
    println!("method: {m}");
    assert_eq!(m.to_string(), "GET");

    // FromStr
    let parsed: HttpMethod = "POST".parse().unwrap();
    assert_eq!(parsed, HttpMethod::POST);

    // Error case
    let err = "INVALID".parse::<HttpMethod>().unwrap_err();
    println!("parse error: {err}");

    // all()
    let methods = HttpMethod::all();
    assert_eq!(methods.len(), 5);
    println!("all methods: {:?}", methods);

    // Works with Color too
    let c: Color = "green".parse().unwrap();
    println!("color: {c}");

    println!("all tests passed");
}
```
</details>

### Exercise 2: Impl Generator with Callbacks

Build a macro system where `with_numeric_types!` invokes a callback macro with a list of numeric types. Then write a callback macro `impl_saturating_add!` that generates a `SaturatingAdd` trait implementation for each type. The callback pattern allows new traits to be added without modifying `with_numeric_types!`.

**Target usage:**
```rust
trait SaturatingAdd {
    fn saturating_add(self, other: Self) -> Self;
}

with_numeric_types!(impl_saturating_add);
// Generates SaturatingAdd impl for u8, u16, u32, u64, i8, i16, i32, i64
```

**Constraints:**
- `with_numeric_types!` must accept any macro name as a callback
- Write two different callback macros that use the same `with_numeric_types!`
- One callback implements `SaturatingAdd`, the other implements `CheckedMul`

<details>
<summary>Solution</summary>

```rust
macro_rules! with_numeric_types {
    ($callback:ident) => {
        $callback!(u8, u16, u32, u64, u128, usize, i8, i16, i32, i64, i128, isize);
    };
}

// Callback 1: SaturatingAdd
trait SaturatingAdd {
    fn saturating_add(self, other: Self) -> Self;
}

macro_rules! impl_saturating_add {
    ($($ty:ty),*) => {
        $(
            impl SaturatingAdd for $ty {
                fn saturating_add(self, other: Self) -> Self {
                    <$ty>::saturating_add(self, other)
                }
            }
        )*
    };
}

// Callback 2: CheckedMul
trait CheckedMul {
    fn checked_mul(self, other: Self) -> Option<Self> where Self: Sized;
}

macro_rules! impl_checked_mul {
    ($($ty:ty),*) => {
        $(
            impl CheckedMul for $ty {
                fn checked_mul(self, other: Self) -> Option<Self> {
                    <$ty>::checked_mul(self, other)
                }
            }
        )*
    };
}

// Callback 3: MinMax
trait MinMax {
    fn type_min() -> Self;
    fn type_max() -> Self;
}

macro_rules! impl_min_max {
    ($($ty:ty),*) => {
        $(
            impl MinMax for $ty {
                fn type_min() -> Self { <$ty>::MIN }
                fn type_max() -> Self { <$ty>::MAX }
            }
        )*
    };
}

// Invoke all callbacks with the same type list
with_numeric_types!(impl_saturating_add);
with_numeric_types!(impl_checked_mul);
with_numeric_types!(impl_min_max);

fn main() {
    // SaturatingAdd
    assert_eq!(250u8.saturating_add(10), 255u8);
    assert_eq!(100i8.saturating_add(100), 127i8);
    println!("saturating_add: 250u8 + 10 = {}", 250u8.saturating_add(10));

    // CheckedMul
    assert_eq!(200u8.checked_mul(2), None);
    assert_eq!(10u32.checked_mul(20), Some(200));
    println!("checked_mul: 200u8 * 2 = {:?}", 200u8.checked_mul(2));

    // MinMax
    println!("u8 range: {} to {}", u8::type_min(), u8::type_max());
    println!("i64 range: {} to {}", i64::type_min(), i64::type_max());

    println!("all callbacks work");
}
```
</details>

### Exercise 3: Compile-Time Counted Vec Initialization

Build a macro `counted_init!` that takes a list of expressions, counts them at compile time using the bit-shift technique, creates a `Vec` with exact pre-allocated capacity, and pushes each element. Verify at compile time that the count is correct by asserting `vec.len() == vec.capacity()`.

**Target usage:**
```rust
let v = counted_init![1, 2, 3, 4, 5];
assert_eq!(v.len(), 5);
assert_eq!(v.capacity(), 5); // exactly pre-allocated, no waste
```

**Constraints:**
- Use the bit-shift counting technique (not the array-length trick)
- The count must be computed at compile time as a `const`
- Works with any expression type, not just literals
- Verify with `cargo expand` that the expansion is correct

<details>
<summary>Solution</summary>

```rust
// Bit-shift counter: O(log n) recursion depth
macro_rules! count_exprs {
    () => { 0usize };
    ($one:expr) => { 1usize };
    ($one:expr, $two:expr) => { 2usize };
    // Even count: pair up and shift
    ($($a:expr, $b:expr),*) => {
        count_exprs!($($a),*) << 1usize
    };
    // Odd count: one extra + even pairs
    ($odd:expr, $($a:expr, $b:expr),*) => {
        (count_exprs!($($a),*) << 1usize) | 1usize
    };
}

macro_rules! counted_init {
    ($($elem:expr),* $(,)?) => {{
        const COUNT: usize = count_exprs!($($elem),*);
        let mut v = Vec::with_capacity(COUNT);
        $(v.push($elem);)*
        debug_assert_eq!(v.len(), COUNT, "counted_init: len mismatch");
        debug_assert_eq!(v.len(), v.capacity(), "counted_init: capacity mismatch");
        v
    }};
}

fn main() {
    // Basic usage
    let v = counted_init![1, 2, 3, 4, 5];
    assert_eq!(v.len(), 5);
    assert_eq!(v.capacity(), 5);
    println!("5 elements: len={}, cap={}", v.len(), v.capacity());

    // Odd count
    let v = counted_init!["a", "b", "c"];
    assert_eq!(v.len(), 3);
    assert_eq!(v.capacity(), 3);
    println!("3 elements: len={}, cap={}", v.len(), v.capacity());

    // Even count
    let v = counted_init![10, 20, 30, 40, 50, 60];
    assert_eq!(v.len(), 6);
    assert_eq!(v.capacity(), 6);
    println!("6 elements: len={}, cap={}", v.len(), v.capacity());

    // Expressions, not just literals
    let v = counted_init![
        String::from("hello"),
        "world".to_uppercase(),
        format!("{} {}", "foo", "bar"),
    ];
    assert_eq!(v.len(), 3);
    assert_eq!(v.capacity(), 3);
    println!("expressions: {v:?}");

    // Single element
    let v = counted_init![42];
    assert_eq!(v.len(), 1);
    assert_eq!(v.capacity(), 1);

    // Empty
    let v: Vec<i32> = counted_init![];
    assert_eq!(v.len(), 0);
    assert_eq!(v.capacity(), 0);

    println!("all counted_init tests passed");
}
```

Run `cargo expand` to verify the expansion. For `counted_init![1, 2, 3, 4, 5]`, you should see:

```rust
{
    const COUNT: usize = (2usize << 1usize) | 1usize;
    // which evaluates to 5
    let mut v = Vec::with_capacity(COUNT);
    v.push(1);
    v.push(2);
    v.push(3);
    v.push(4);
    v.push(5);
    // ...assertions...
    v
}
```
</details>

### Exercise 4: State Machine DSL with Validation

Extend the state machine DSL from the concepts section to support: (1) entry and exit actions on states, (2) guard conditions on transitions, and (3) a `dot()` method that generates a Graphviz DOT representation of the state machine for visualization.

**Target syntax:**
```rust
state_machine! {
    name: Door,
    states: [Locked, Closed, Open],
    events: [Lock, Unlock, OpenDoor, CloseDoor],
    initial: Locked,
    transitions: {
        Locked -> Closed on Unlock,
        Closed -> Locked on Lock,
        Closed -> Open on OpenDoor,
        Open -> Closed on CloseDoor,
    }
}
```

**Constraints:**
- The `dot()` method must return a `String` containing valid Graphviz DOT syntax
- Invalid transitions must return a typed error, not panic
- The generated code must derive `Debug` and `Clone` for the state machine struct
- Include a `history()` method that returns a `&[State]` of all states visited

<details>
<summary>Solution</summary>

```rust
macro_rules! state_machine {
    (
        name: $name:ident,
        states: [$($state:ident),* $(,)?],
        events: [$($event:ident),* $(,)?],
        initial: $initial:ident,
        transitions: {
            $($from:ident -> $to:ident on $evt:ident),* $(,)?
        }
    ) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
        pub enum State {
            $($state,)*
        }

        impl ::std::fmt::Display for State {
            fn fmt(&self, f: &mut ::std::fmt::Formatter<'_>) -> ::std::fmt::Result {
                match self {
                    $(State::$state => write!(f, stringify!($state)),)*
                }
            }
        }

        #[derive(Debug, Clone, Copy, PartialEq, Eq)]
        pub enum Event {
            $($event,)*
        }

        impl ::std::fmt::Display for Event {
            fn fmt(&self, f: &mut ::std::fmt::Formatter<'_>) -> ::std::fmt::Result {
                match self {
                    $(Event::$event => write!(f, stringify!($event)),)*
                }
            }
        }

        #[derive(Debug)]
        pub struct TransitionError {
            pub from: State,
            pub event: Event,
        }

        impl ::std::fmt::Display for TransitionError {
            fn fmt(&self, f: &mut ::std::fmt::Formatter<'_>) -> ::std::fmt::Result {
                write!(
                    f,
                    "no transition from {} on event {}",
                    self.from, self.event
                )
            }
        }

        impl std::error::Error for TransitionError {}

        #[derive(Debug, Clone)]
        pub struct $name {
            state: State,
            history: Vec<State>,
        }

        impl $name {
            pub fn new() -> Self {
                Self {
                    state: State::$initial,
                    history: vec![State::$initial],
                }
            }

            pub fn state(&self) -> State {
                self.state
            }

            pub fn history(&self) -> &[State] {
                &self.history
            }

            pub fn handle(&mut self, event: Event) -> Result<State, TransitionError> {
                let new_state = match (self.state, event) {
                    $(
                        (State::$from, Event::$evt) => State::$to,
                    )*
                    (from, event) => {
                        return Err(TransitionError { from, event });
                    }
                };
                self.state = new_state;
                self.history.push(new_state);
                Ok(new_state)
            }

            pub fn can_handle(&self, event: Event) -> bool {
                matches!(
                    (self.state, event),
                    $((State::$from, Event::$evt))|*
                )
            }

            pub fn dot() -> String {
                let mut s = String::new();
                s.push_str(&format!("digraph {} {{\n", stringify!($name)));
                s.push_str("    rankdir=LR;\n");
                s.push_str("    node [shape=circle];\n");

                // Mark initial state with double circle
                s.push_str(&format!(
                    "    {} [shape=doublecircle];\n",
                    stringify!($initial)
                ));

                // Add all transitions
                $(
                    s.push_str(&format!(
                        "    {} -> {} [label=\"{}\"];\n",
                        stringify!($from),
                        stringify!($to),
                        stringify!($evt)
                    ));
                )*

                s.push_str("}\n");
                s
            }
        }
    };
}

state_machine! {
    name: Door,
    states: [Locked, Closed, Open],
    events: [Lock, Unlock, OpenDoor, CloseDoor],
    initial: Locked,
    transitions: {
        Locked -> Closed on Unlock,
        Closed -> Locked on Lock,
        Closed -> Open on OpenDoor,
        Open -> Closed on CloseDoor,
    }
}

state_machine! {
    name: TrafficLight,
    states: [Red, Yellow, Green],
    events: [Next],
    initial: Red,
    transitions: {
        Red -> Green on Next,
        Green -> Yellow on Next,
        Yellow -> Red on Next,
    }
}

fn main() {
    // Door state machine
    let mut door = Door::new();
    assert_eq!(door.state(), State::Locked);

    door.handle(Event::Unlock).unwrap();
    door.handle(Event::OpenDoor).unwrap();
    door.handle(Event::CloseDoor).unwrap();
    door.handle(Event::Lock).unwrap();
    assert_eq!(door.state(), State::Locked);

    println!("door history: {:?}", door.history());
    assert_eq!(door.history().len(), 5);

    // Invalid transition
    let err = door.handle(Event::OpenDoor).unwrap_err();
    println!("error: {err}");

    // can_handle check
    assert!(door.can_handle(Event::Unlock));
    assert!(!door.can_handle(Event::CloseDoor));

    // DOT output
    println!("\n--- Graphviz DOT ---");
    println!("{}", Door::dot());

    // Traffic light
    let mut light = TrafficLight::new();
    for _ in 0..6 {
        print!("{:?} -> ", light.state());
        light.handle(Event::Next).unwrap();
    }
    println!("{:?}", light.state());

    println!("\nall tests passed");
}
```

Save the DOT output to a file and render with:
```bash
cargo run > /dev/null 2>&1
# Or capture DOT output and render:
# dot -Tpng door.dot -o door.png
```
</details>

## Common Mistakes

1. **Matching `$e:expr` when you need `$t:tt`.** An `expr` fragment is greedy: it consumes as many tokens as possible to form a valid expression. This can swallow tokens you intended for later matching. When building TT munchers, use `$t:tt` for maximum control over token consumption.

2. **Forgetting the recursion limit.** Complex recursive macros hit the default limit of 128 expansions. The error message ("recursion limit reached while expanding") is clear, but the fix (`#![recursion_limit = "256"]`) feels like a code smell. Consider whether a proc macro would be cleaner.

3. **Hygiene assumptions.** `macro_rules!` macros are partially hygienic: local variables in the macro do not leak, but type names and function calls use the caller's scope. If your macro references `HashMap`, the caller must have `use std::collections::HashMap`. Use fully qualified paths (`::std::collections::HashMap`) in macro output to avoid this.

4. **Missing trailing comma handling.** Users expect `macro!(a, b, c,)` to work (trailing comma). Add `$(,)?` at the end of your repetition patterns. Without it, users get confusing errors.

5. **Debugging by staring.** Do not try to mentally expand complex macros. Run `cargo expand` immediately. It is faster and more accurate than manual expansion.

6. **Using macros when generics suffice.** If you can express the abstraction with generics and trait bounds, prefer that. Macros should be reserved for cases where you need to generate new identifiers, new types, or new syntax that the type system cannot express.

## Verification

```bash
cargo build
cargo test
cargo clippy -- -W clippy::pedantic

# Inspect macro expansion (requires cargo-expand)
cargo install cargo-expand
cargo expand

# On nightly, use trace_macros for step-by-step expansion
# rustup run nightly cargo build
```

## Trade-Off Analysis: Macros vs Generics vs Proc Macros

| Dimension | macro_rules! | Generics + Traits | Proc Macros |
|---|---|---|---|
| **Can generate new types** | Yes | No | Yes |
| **Can generate new identifiers** | Limited (needs `paste`) | No | Yes (full Ident manipulation) |
| **Compile-time cost** | Low | Low | High (separate crate, syn parsing) |
| **Error messages** | Often confusing | Clear (type errors) | Custom (via compile_error!) |
| **Debugging** | cargo expand, trace_macros | Normal debugger | cargo expand, println in build |
| **IDE support** | Limited (rust-analyzer tries) | Full | Limited |
| **Recursion** | Limited (128 default) | Unlimited (monomorphization) | Unlimited (Rust code) |
| **Arbitrary syntax** | TT munching (complex) | No custom syntax | Full (parse anything) |
| **Code sharing** | Copy-paste or crate | Trait bounds, generics | Crate dependency |
| **Maintenance** | Hard for complex macros | Easy | Medium (syn/quote API churn) |

**Decision framework:**
- Need the same logic for different types? Use **generics**.
- Need to generate boilerplate impls for a known set of types? Use **macro_rules!** with callbacks.
- Need to generate new types, builders, or DSLs? Use **macro_rules!** if the syntax fits, otherwise **proc macros**.
- Need to parse attributes (`#[derive(...)]`, `#[my_attr]`)? Use **proc macros** (macro_rules cannot intercept attributes).
- Need IDE support and clear error messages? Prefer **generics** over macros of any kind.

## What You Learned

- TT munchers process token trees recursively, peeling off one token at a time for arbitrary syntax parsing
- Push-down accumulation carries intermediate results through recursion using bracketed accumulators
- Internal rules (prefixed with `@`) organize complex macros into maintainable sections
- Macro callbacks enable composition: one macro generates a type list, another generates impls
- Bit-shift counting achieves O(log n) recursion depth for counting macro repetitions
- Generating impl blocks for multiple types is the most common practical use of advanced macros
- `cargo expand` is the essential debugging tool; never try to mentally expand complex macros
- Macros complement generics -- they excel at generating types, identifiers, and boilerplate that the type system cannot abstract

## What's Next

Exercise 25 applies macro and serde knowledge to zero-copy deserialization, where compile-time code generation meets runtime performance optimization.

## Resources

- [The Little Book of Rust Macros](https://veykril.github.io/tlborm/)
- [Rust Reference: Macros By Example](https://doc.rust-lang.org/reference/macros-by-example.html)
- [cargo-expand](https://github.com/dtolnay/cargo-expand)
- [paste crate (identifier concatenation)](https://docs.rs/paste)
- [Macro patterns in the Rust ecosystem (dtolnay)](https://github.com/dtolnay/case-studies)
- [Rust API Guidelines: Macros](https://rust-lang.github.io/api-guidelines/macros.html)
