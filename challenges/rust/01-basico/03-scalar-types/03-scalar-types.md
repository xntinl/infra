# 3. Scalar Types

**Difficulty**: Basico

## Prerequisites

- Completed exercises 1-2 (Hello Rust, Variables and Mutability)
- Understanding of `let` bindings, `mut`, and type annotations

## Learning Objectives

After completing this exercise, you will be able to:

- Identify all Rust integer types and choose the appropriate one for a given use case
- Explain how integer overflow behaves in debug vs release builds
- Use floating-point types and understand their precision limitations
- Work with booleans and the `char` type (4-byte Unicode scalar)
- Write numeric literals in hex, octal, binary, and byte formats
- Apply type inference and explicit type annotations

## Concepts

### What Is a Scalar Type?

A **scalar** type represents a single value. Rust has four scalar categories: integers, floating-point numbers, booleans, and characters. If you come from C, these map directly. From Python or JavaScript, these are the primitive types that live on the stack and have a fixed, known size at compile time.

### Integer Types

Rust provides integers in explicit bit widths. No guessing, no platform-dependent surprises:

| Signed | Unsigned | Bits | Range |
|---|---|---|---|
| `i8` | `u8` | 8 | -128..127 / 0..255 |
| `i16` | `u16` | 16 | -32,768..32,767 / 0..65,535 |
| `i32` | `u32` | 32 | -2.1B..2.1B / 0..4.2B |
| `i64` | `u64` | 64 | Very large |
| `i128` | `u128` | 128 | Extremely large |
| `isize` | `usize` | Platform | 32 or 64 bits |

**Signed** (`i`) means the number can be negative (uses two's complement, same as C). **Unsigned** (`u`) means zero and positive only.

**`i32`** is the default — when you write `let x = 42;`, the compiler infers `i32`. This is a good general-purpose choice: 32 bits is fast on all modern hardware and covers most use cases.

**`isize` and `usize`** match your CPU's pointer width — 64 bits on a 64-bit machine, 32 bits on a 32-bit machine. `usize` is the type used for indexing collections and anything related to memory sizes. You will see it constantly.

### Integer Overflow

In C, signed integer overflow is undefined behavior — the compiler can do anything. Rust is explicit about it:

- **Debug builds** (`cargo build`): overflow causes a **panic** — the program crashes immediately with an error message. This catches bugs during development.
- **Release builds** (`cargo build --release`): overflow wraps around silently using two's complement. For example, `255u8 + 1` becomes `0`.

If you need specific overflow behavior, Rust provides explicit methods: `wrapping_add`, `checked_add` (returns `None` on overflow), `saturating_add` (clamps at max), and `overflowing_add` (returns value plus a boolean).

### Floating-Point Types

Rust has two floating-point types, both following IEEE 754:

- **`f64`**: 64-bit double precision. The default — when you write `let x = 3.14;`, you get `f64`.
- **`f32`**: 32-bit single precision. Use only when you have a specific reason (GPU interop, memory constraints).

Floating-point arithmetic has the same precision limitations as every other language:

```rust
let result = 0.1 + 0.2; // 0.30000000000000004, not 0.3
```

This is not a Rust bug — it is inherent to IEEE 754. If you need exact decimal arithmetic (financial calculations), use a decimal crate like `rust_decimal`.

### Boolean

The `bool` type has two values: `true` and `false`. It is one byte in size. Booleans are used in conditions (`if`, `while`) and are the result of comparison operators (`==`, `!=`, `<`, `>`).

```rust
let is_active: bool = true;
```

Unlike C, Rust does not implicitly convert integers to booleans. `if 1 { ... }` is a compiler error — you must write `if 1 != 0 { ... }` or use an actual `bool`.

### The char Type

Rust's `char` represents a **Unicode Scalar Value** and is always 4 bytes (32 bits), regardless of the character. This is different from C's `char` (1 byte, ASCII only) and Java's `char` (2 bytes, UTF-16).

```rust
let letter = 'A';
let emoji = '\u{1F600}'; // grinning face
let kanji = '\u{5B57}';  // the kanji character for "character"
```

Note: `char` uses single quotes. Double quotes create a string (`&str`), not a `char`.

### Numeric Literals

Rust lets you write numbers in multiple bases and formats:

| Format | Example | Value |
|---|---|---|
| Decimal | `1_000_000` | 1000000 |
| Hex | `0xff` | 255 |
| Octal | `0o77` | 63 |
| Binary | `0b1111_0000` | 240 |
| Byte (u8) | `b'A'` | 65 |

Underscores (`_`) are visual separators — the compiler ignores them. Use them to make large numbers readable: `1_000_000` is clearer than `1000000`.

You can also attach a type suffix directly to a literal: `42u8`, `3.14f32`, `1_000i64`.

## Exercises

### Exercise 1: Integer Types and Inference

Create a new project:

```
$ cargo new scalar-types-lab
$ cd scalar-types-lab
```

Edit `src/main.rs`:

```rust
fn main() {
    // Default inference: i32
    let default_int = 42;

    // Explicit type annotations
    let small: i8 = 127;
    let byte: u8 = 255;
    let large: i64 = 9_223_372_036_854_775_807;
    let index: usize = 0;

    // Type suffix on literal
    let explicit_suffix = 100u32;

    println!("default_int (i32): {}", default_int);
    println!("small (i8): {}", small);
    println!("byte (u8): {}", byte);
    println!("large (i64): {}", large);
    println!("index (usize): {}", index);
    println!("explicit_suffix (u32): {}", explicit_suffix);

    // Size in bytes using std::mem::size_of_val
    println!("\n--- Sizes ---");
    println!("i8:    {} byte", std::mem::size_of_val(&small));
    println!("u8:    {} byte", std::mem::size_of_val(&byte));
    println!("i32:   {} bytes", std::mem::size_of_val(&default_int));
    println!("i64:   {} bytes", std::mem::size_of_val(&large));
    println!("usize: {} bytes", std::mem::size_of_val(&index));
}
```

**What's happening here:**

1. `let default_int = 42` — the compiler infers `i32` because that is the default for unsuffixed integer literals.
2. `let small: i8 = 127` — explicit annotation. `i8` ranges from -128 to 127, so 127 is the maximum.
3. `let byte: u8 = 255` — `u8` ranges from 0 to 255. This is the type used for raw bytes.
4. `std::mem::size_of_val` returns the size in bytes of the value. This is a standard library function, not a macro.

What do you think this will print?

```
$ cargo run
default_int (i32): 42
small (i8): 127
byte (u8): 255
large (i64): 9223372036854775807
index (usize): 0
explicit_suffix (u32): 100

--- Sizes ---
i8:    1 byte
u8:    1 byte
i32:   4 bytes
i64:   8 bytes
usize: 8 bytes
```

The `usize` size depends on your platform — 8 bytes on 64-bit, 4 bytes on 32-bit.

### Exercise 2: Integer Overflow

Edit `src/main.rs`:

```rust
fn main() {
    let max_u8: u8 = 255;
    println!("max u8 value: {}", max_u8);

    // Wrapping arithmetic: explicit and safe
    let wrapped = max_u8.wrapping_add(1);
    println!("255u8.wrapping_add(1) = {}", wrapped);

    // Checked arithmetic: returns None on overflow
    let checked = max_u8.checked_add(1);
    println!("255u8.checked_add(1) = {:?}", checked);

    let safe_check = 200u8.checked_add(10);
    println!("200u8.checked_add(10) = {:?}", safe_check);

    // Saturating arithmetic: clamps at the boundary
    let saturated = max_u8.saturating_add(100);
    println!("255u8.saturating_add(100) = {}", saturated);

    // Overflowing arithmetic: returns (value, did_overflow)
    let (result, overflowed) = max_u8.overflowing_add(1);
    println!("255u8.overflowing_add(1) = ({}, {})", result, overflowed);
}
```

**What's happening here:**

1. `wrapping_add` does modular arithmetic: 255 + 1 wraps to 0 for `u8`.
2. `checked_add` returns `Option<u8>` — either `Some(value)` or `None` if overflow occurs. The `{:?}` format prints the debug representation of the `Option`.
3. `saturating_add` clamps at the maximum: 255 + 100 stays at 255 for `u8`.
4. `overflowing_add` returns a tuple: the wrapped result and a boolean indicating whether overflow occurred.

What do you think this will print?

```
$ cargo run
max u8 value: 255
255u8.wrapping_add(1) = 0
255u8.checked_add(1) = None
200u8.checked_add(10) = Some(210)
255u8.saturating_add(100) = 255
255u8.overflowing_add(1) = (0, true)
```

These methods make overflow handling explicit. In production code, prefer `checked_add` or `saturating_add` over raw `+` when overflow is a realistic possibility.

### Exercise 3: Floating Point and Numeric Literals

Edit `src/main.rs`:

```rust
fn main() {
    // Floating-point defaults to f64
    let pi = 3.141592653589793;
    let gravity: f32 = 9.81;

    println!("pi (f64): {}", pi);
    println!("gravity (f32): {}", gravity);
    println!("f64 size: {} bytes", std::mem::size_of_val(&pi));
    println!("f32 size: {} bytes", std::mem::size_of_val(&gravity));

    // Precision difference
    let precise: f64 = 1.0 / 3.0;
    let imprecise: f32 = 1.0 / 3.0;
    println!("\n1/3 as f64: {:.20}", precise);
    println!("1/3 as f32: {:.20}", imprecise);

    // Numeric literals in different bases
    let decimal = 1_000_000;
    let hex = 0xFF;
    let octal = 0o77;
    let binary = 0b1111_0000;
    let byte_literal = b'A';

    println!("\n--- Numeric Literals ---");
    println!("decimal:  {}", decimal);
    println!("hex 0xFF: {}", hex);
    println!("octal 0o77: {}", octal);
    println!("binary 0b1111_0000: {}", binary);
    println!("byte b'A': {}", byte_literal);

    // Arithmetic
    let sum = 5 + 10;
    let difference = 95.5 - 4.3;
    let product = 4 * 30;
    let quotient = 56.7 / 32.2;
    let truncated = -5 / 3; // integer division truncates toward zero
    let remainder = 43 % 5;

    println!("\n--- Arithmetic ---");
    println!("5 + 10 = {}", sum);
    println!("95.5 - 4.3 = {}", difference);
    println!("4 * 30 = {}", product);
    println!("56.7 / 32.2 = {}", quotient);
    println!("-5 / 3 = {}", truncated);
    println!("43 % 5 = {}", remainder);
}
```

**What's happening here:**

1. `{:.20}` formats the float with 20 decimal places, revealing precision differences between `f32` and `f64`.
2. Integer division truncates toward zero — `-5 / 3` is `-1`, not `-2`. Same behavior as C and Java.
3. All numeric literals (`0xFF`, `0o77`, `0b1111_0000`) produce regular integers. The base is just a notation convenience.
4. You cannot mix integer and float arithmetic directly: `5 + 3.14` is a compiler error. You must cast explicitly.

What do you think this will print?

```
$ cargo run
pi (f64): 3.141592653589793
gravity (f32): 9.81
f64 size: 8 bytes
f32 size: 4 bytes

1/3 as f64: 0.33333333333333331483
1/3 as f32: 0.33333334326744079590

--- Numeric Literals ---
decimal:  1000000
hex 0xFF: 255
octal 0o77: 63
binary 0b1111_0000: 240
byte b'A': 65

--- Arithmetic ---
5 + 10 = 15
95.5 - 4.3 = 91.2
4 * 30 = 120
56.7 / 32.2 = 1.7608695652173911
-5 / 3 = -1
43 % 5 = 3
```

### Exercise 4: Booleans and Characters

Edit `src/main.rs`:

```rust
fn main() {
    // Booleans
    let is_active: bool = true;
    let is_greater = 10 > 5;
    let is_equal = 42 == 42;

    println!("is_active: {}", is_active);
    println!("10 > 5: {}", is_greater);
    println!("42 == 42: {}", is_equal);
    println!("bool size: {} byte", std::mem::size_of::<bool>());

    // Characters (4 bytes, Unicode Scalar Value)
    let letter = 'z';
    let number_char = '9';
    let space = ' ';
    let unicode_char = '\u{00E9}'; // e with acute accent
    let cjk_char = '\u{4E16}';    // Chinese character for "world"

    println!("\n--- Characters ---");
    println!("letter: {}", letter);
    println!("number_char: {}", number_char);
    println!("space: '{}'", space);
    println!("unicode \\u{{00E9}}: {}", unicode_char);
    println!("CJK \\u{{4E16}}: {}", cjk_char);
    println!("char size: {} bytes", std::mem::size_of::<char>());

    // char is NOT the same as a string of length 1
    let c: char = 'A';        // single quotes, char, 4 bytes
    let s: &str = "A";        // double quotes, &str, a string slice
    println!("\nchar 'A' size: {} bytes", std::mem::size_of_val(&c));
    println!("&str \"A\" size: {} bytes (pointer + length)", std::mem::size_of_val(&s));
}
```

**What's happening here:**

1. Comparison operators (`>`, `==`, `!=`, `<`, `>=`, `<=`) return `bool`.
2. `std::mem::size_of::<bool>()` uses the **turbofish** syntax `::<Type>` to specify a type parameter. It returns the size of the type itself, not a specific value.
3. Every `char` is 4 bytes because it can represent any Unicode Scalar Value (U+0000 to U+D7FF and U+E000 to U+10FFFF).
4. `'A'` (char) and `"A"` (string) are fundamentally different types with different sizes and representations.

What do you think this will print?

```
$ cargo run
is_active: true
10 > 5: true
42 == 42: true
bool size: 1 byte

--- Characters ---
letter: z
number_char: 9
space: ' '
unicode \u{00E9}: e with acute accent
CJK \u{4E16}: Chinese character
char size: 4 bytes

char 'A' size: 4 bytes
&str "A" size: 16 bytes (pointer + length)
```

Note: `&str` is 16 bytes on a 64-bit system because it is a **fat pointer** — 8 bytes for the pointer to the data and 8 bytes for the length. This is a preview of Rust's ownership system that we will cover in later exercises.

### Exercise 5: Type Casting and Conversion

Edit `src/main.rs`:

```rust
fn main() {
    // Rust requires explicit casts between numeric types
    let integer: i32 = 42;
    let float: f64 = integer as f64;
    println!("i32 {} as f64: {}", integer, float);

    let big: i64 = 1_000_000;
    let small: i16 = big as i16;
    println!("i64 {} as i16: {} (truncated!)", big, small);

    let negative: i32 = -1;
    let unsigned: u32 = negative as u32;
    println!("i32 {} as u32: {} (reinterpreted bits)", negative, unsigned);

    let float_val: f64 = 3.99;
    let truncated: i32 = float_val as i32;
    println!("f64 {} as i32: {} (truncated, not rounded)", float_val, truncated);

    // char to number and back
    let ch = 'A';
    let code = ch as u32;
    println!("\n'{}' as u32: {}", ch, code);

    let code_point: u8 = 66;
    let from_code = code_point as char;
    println!("u8 {} as char: '{}'", code_point, from_code);

    // Boolean to integer
    let flag = true;
    let as_int = flag as i32;
    println!("\ntrue as i32: {}", as_int);
    println!("false as i32: {}", false as i32);
}
```

**What's happening here:**

1. `as` performs type casting. Unlike C, there is no implicit conversion — you must write `as` every time.
2. Casting `i64` to `i16` silently truncates the value (keeps only the lower 16 bits). This is a known footgun — `1_000_000i64 as i16` gives `16960` because only the lower bits fit.
3. Casting a negative `i32` to `u32` reinterprets the two's complement bits as unsigned: `-1i32` becomes `4294967295u32`.
4. Casting `f64` to `i32` truncates toward zero — `3.99` becomes `3`, not `4`.
5. `char` can be cast to `u32` to get its Unicode code point, and small integers can be cast back to `char`.

What do you think this will print?

```
$ cargo run
i32 42 as f64: 42
i64 1000000 as i16: 16960 (truncated!)
i32 -1 as u32: 4294967295 (reinterpreted bits)
f64 3.99 as i32: 3 (truncated, not rounded)

'A' as u32: 65
u8 66 as char: 'B'

true as i32: 1
false as i32: 0
```

## Common Mistakes

### Mixing Integer and Float Arithmetic

```rust
fn main() {
    let x = 5;
    let y = 3.0;
    let result = x + y;
}
```

```
error[E0277]: cannot add `{float}` to `{integer}`
 --> src/main.rs:4:20
  |
4 |     let result = x + y;
  |                    ^ no implementation for `{integer} + {float}`
```

**Why:** Rust never does implicit numeric conversion. This prevents subtle precision loss bugs that plague C and JavaScript.
**Fix:** Cast explicitly: `let result = x as f64 + y;`

### Integer Overflow in Debug Mode

```rust
fn main() {
    let x: u8 = 255;
    let y: u8 = x + 1;
    println!("{}", y);
}
```

```
thread 'main' panicked at 'attempt to add with overflow', src/main.rs:3:20
```

**Why:** In debug builds, Rust panics on overflow to catch bugs early. This is a feature, not a bug.
**Fix:** Use `wrapping_add`, `checked_add`, or `saturating_add` depending on the behavior you want.

### Double Quotes for char

```rust
fn main() {
    let c: char = "A";
}
```

```
error[E0308]: mismatched types
 --> src/main.rs:2:19
  |
2 |     let c: char = "A";
  |            ----   ^^^ expected `char`, found `&str`
  |            |
  |            expected due to this
```

**Why:** Single quotes (`'A'`) create a `char`. Double quotes (`"A"`) create a `&str`. They are different types.
**Fix:** `let c: char = 'A';`

### Using usize for General Math

```rust
fn main() {
    let a: usize = 5;
    let b: usize = 10;
    let result = a - b; // panics in debug: attempt to subtract with overflow
}
```

**Why:** `usize` is unsigned. Subtracting a larger value from a smaller one overflows. Use `i32` or `i64` for general arithmetic. Reserve `usize` for indexing and sizes.
**Fix:** `let a: i32 = 5; let b: i32 = 10; let result = a - b;` gives `-5`.

## Verification

Run these commands from your project directory:

```
$ cargo check
    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.00s
```

```
$ cargo run
i32 42 as f64: 42
i64 1000000 as i16: 16960 (truncated!)
i32 -1 as u32: 4294967295 (reinterpreted bits)
f64 3.99 as i32: 3 (truncated, not rounded)

'A' as u32: 65
u8 66 as char: 'B'

true as i32: 1
false as i32: 0
```

```
$ cargo build --release
    Finished `release` profile [optimized] target(s) in 0.00s
```

If all commands succeed with the expected output, you have completed this exercise.

## Summary

- **Key concepts**: Four scalar types (integers, floats, booleans, chars), explicit bit widths (`i8` through `i128`), `usize` for indexing, type inference defaults (`i32`, `f64`), overflow handling methods, `as` for explicit casting
- **What you practiced**: Declaring typed variables, numeric literals in multiple bases, using underscores for readability, exploring memory sizes, explicit type casting, understanding overflow behavior
- **Important to remember**: Rust defaults to `i32` for integers and `f64` for floats. There are no implicit conversions — use `as` to cast. Integer overflow panics in debug mode. Use `usize` for indexing, `i32`/`i64` for general math. `char` is 4 bytes (Unicode), not 1 byte (ASCII).

## What's Next

Scalar types represent single values. In the next exercise, we will combine multiple values into **compound types** — tuples and arrays — and explore how Rust handles fixed-size collections on the stack.

## Resources

- [The Rust Programming Language — Chapter 3.2: Data Types](https://doc.rust-lang.org/book/ch03-02-data-types.html)
- [Rust by Example — Primitives](https://doc.rust-lang.org/rust-by-example/primitives.html)
- [Rust Reference — Types: Numeric](https://doc.rust-lang.org/reference/types/numeric.html)
