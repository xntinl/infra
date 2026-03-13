# 12. Operator Overloading

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-basico exercises (scalar types, compound types, functions)
- Completed: 01-traits, 02-generics, 11-type-conversions
- Comfortable implementing traits and using generics

## Learning Objectives

- Implement `std::ops` traits (`Add`, `Sub`, `Mul`, `Neg`, `Index`) to overload operators for custom types
- Implement `Display` and `Debug` for human-readable and developer-facing output
- Analyze the difference between `PartialEq`/`Eq` and `PartialOrd`/`Ord`
- Evaluate why floating-point types implement `PartialEq` but not `Eq`
- Apply formatting macros and format specifiers for custom output

## Concepts

### Why Operator Overloading in Rust?

In many languages, operator overloading is considered dangerous because it lets you make `+` do something completely unrelated to addition. Rust takes a pragmatic middle ground: operators are just syntactic sugar for trait method calls, and the trait names make the intent clear. `a + b` calls `a.add(b)`. If your type genuinely represents something addable, implementing `Add` makes the code more readable, not less.

The key insight: every operator in Rust maps to a trait in `std::ops`. Implement the trait, and your type gains that operator.

### Arithmetic Operators

```rust
use std::ops::{Add, Sub, Neg};

#[derive(Debug, Clone, Copy, PartialEq)]
struct Vec2 {
    x: f64,
    y: f64,
}

impl Add for Vec2 {
    type Output = Vec2;

    fn add(self, rhs: Vec2) -> Vec2 {
        Vec2 {
            x: self.x + rhs.x,
            y: self.y + rhs.y,
        }
    }
}

impl Neg for Vec2 {
    type Output = Vec2;

    fn neg(self) -> Vec2 {
        Vec2 { x: -self.x, y: -self.y }
    }
}
```

Notice that `Add` has an associated type `Output`. This means the result of addition does not have to be the same type as the operands. You could implement `Add<f64> for Vec2` where `Output = Vec2` for scalar multiplication disguised as addition -- but you probably should not. Keep operators intuitive.

### The Output Type

The `Output` associated type is powerful. Consider adding a `Meters` to a `Meters` -- the result is `Meters`. But multiplying `Meters * Meters` could produce `SquareMeters`:

```rust
use std::ops::Mul;

struct Meters(f64);
struct SquareMeters(f64);

impl Mul for Meters {
    type Output = SquareMeters;

    fn mul(self, rhs: Meters) -> SquareMeters {
        SquareMeters(self.0 * rhs.0)
    }
}
```

This is type-level dimensional analysis. The compiler prevents you from accidentally adding area to length.

### Index and IndexMut

The `Index` trait lets you use `[]` syntax on your type:

```rust
use std::ops::Index;

struct Matrix {
    data: Vec<Vec<f64>>,
    rows: usize,
    cols: usize,
}

impl Index<(usize, usize)> for Matrix {
    type Output = f64;

    fn index(&self, index: (usize, usize)) -> &f64 {
        &self.data[index.0][index.1]
    }
}

let m = Matrix { /* ... */ };
let val = m[(1, 2)]; // calls m.index((1, 2))
```

### Display vs Debug

These are not operators, but they control how your types appear in format strings and are among the most commonly implemented traits:

```rust
use std::fmt;

struct Point(f64, f64);

// Debug: for developers, usually derived
// Output like: Point(1.0, 2.0)
impl fmt::Debug for Point {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "Point({}, {})", self.0, self.1)
    }
}

// Display: for users, must be implemented manually
// Output like: (1.0, 2.0)
impl fmt::Display for Point {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "({}, {})", self.0, self.1)
    }
}
```

`Debug` is used by `{:?}` and `{:#?}` (pretty-printed). `Display` is used by `{}`. You can derive `Debug` but never `Display` -- the compiler cannot guess your preferred human-readable format.

### Formatting Specifiers

The `fmt::Formatter` receives width, precision, alignment, and fill character from the format string:

```rust
impl fmt::Display for Point {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        // Respect precision if the caller specifies it
        if let Some(precision) = f.precision() {
            write!(f, "({:.prec$}, {:.prec$})", self.0, self.1, prec = precision)
        } else {
            write!(f, "({}, {})", self.0, self.1)
        }
    }
}

// Now these all work:
// format!("{}",     p)  => (1.5, 2.7)
// format!("{:.2}",  p)  => (1.50, 2.70)
// format!("{:>20}", p)  => "           (1.5, 2.7)"
```

### PartialEq, Eq, PartialOrd, Ord

These traits form a hierarchy that controls comparison operators:

| Operator | Trait | Method |
|---|---|---|
| `==`, `!=` | `PartialEq` | `eq(&self, other: &Rhs) -> bool` |
| `<`, `>`, `<=`, `>=` | `PartialOrd` | `partial_cmp(&self, other: &Rhs) -> Option<Ordering>` |

**Partial vs Total**: `PartialEq` does not require that `a == a` for all values. `Eq` (a marker trait with no methods) adds that guarantee. Similarly, `PartialOrd` returns `Option<Ordering>` because some pairs might not be comparable, while `Ord` returns `Ordering` directly -- every pair has a defined order.

The classic example of partial ordering: floating-point numbers. `NaN != NaN` by IEEE 754 rules, so `f64` implements `PartialEq` but not `Eq`. And `NaN` is not less than, greater than, or equal to any value, so `f64` implements `PartialOrd` but not `Ord`.

```rust
let nan = f64::NAN;
println!("{}", nan == nan);   // false
println!("{}", nan < 0.0);    // false
println!("{}", nan > 0.0);    // false
println!("{:?}", nan.partial_cmp(&0.0)); // None
```

This has real consequences: you cannot sort a `Vec<f64>` with `.sort()` (requires `Ord`), only `.sort_by()` with a custom comparator.

### Deriving vs Manual Implementation

For most types, you can derive these traits:

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
struct UserId(u64);
```

Derive `Eq` and `Ord` only when all fields also implement them. A struct containing an `f64` cannot derive `Eq`.

## Exercises

### Exercise 1: Arithmetic for a 2D Vector

```rust
use std::ops::{Add, Sub, Mul, Neg};

#[derive(Debug, Clone, Copy)]
struct Vec2 {
    x: f64,
    y: f64,
}

impl Vec2 {
    fn new(x: f64, y: f64) -> Self {
        Vec2 { x, y }
    }

    fn magnitude(&self) -> f64 {
        (self.x * self.x + self.y * self.y).sqrt()
    }
}

// TODO: Implement Add<Vec2> for Vec2 (component-wise addition)

// TODO: Implement Sub<Vec2> for Vec2 (component-wise subtraction)

// TODO: Implement Neg for Vec2 (negate both components)

// TODO: Implement Mul<f64> for Vec2 (scalar multiplication)
// Output should be Vec2

// TODO: Implement Mul<Vec2> for f64 (so you can write `2.0 * vec` too)
// This lets the scalar go on either side.

fn main() {
    let a = Vec2::new(3.0, 4.0);
    let b = Vec2::new(1.0, 2.0);

    println!("a + b = {:?}", a + b);       // Vec2 { x: 4.0, y: 6.0 }
    println!("a - b = {:?}", a - b);       // Vec2 { x: 2.0, y: 2.0 }
    println!("-a = {:?}", -a);             // Vec2 { x: -3.0, y: -4.0 }
    println!("a * 2.0 = {:?}", a * 2.0);  // Vec2 { x: 6.0, y: 8.0 }
    println!("2.0 * a = {:?}", 2.0 * a);  // Vec2 { x: 6.0, y: 8.0 }
    println!("|a| = {}", a.magnitude());   // 5.0
}
```

### Exercise 2: Display, Debug, and Precision

```rust
use std::fmt;

struct Color {
    r: u8,
    g: u8,
    b: u8,
}

// TODO: Implement Debug for Color manually (not derive).
// Format as: Color { r: 255, g: 128, b: 0 }

// TODO: Implement Display for Color.
// Default format: rgb(255, 128, 0)
// When an alternate flag is used ({:#}): #FF8000 (hex, uppercase)
// Hint: check f.alternate() to detect the # flag.
// Hex formatting: format!("{:02X}", byte) gives uppercase hex.

struct Temperature {
    celsius: f64,
}

// TODO: Implement Display for Temperature.
// Default: "23.50 C" (always 2 decimal places)
// With precision specified by caller: respect it.
// Hint: use f.precision().unwrap_or(2)

fn main() {
    let red = Color { r: 255, g: 0, b: 0 };
    let teal = Color { r: 0, g: 128, b: 128 };

    println!("{:?}", red);      // Color { r: 255, g: 0, b: 0 }
    println!("{}", red);        // rgb(255, 0, 0)
    println!("{:#}", red);      // #FF0000
    println!("{}", teal);       // rgb(0, 128, 128)
    println!("{:#}", teal);     // #008080

    let temp = Temperature { celsius: 23.456 };
    println!("{}", temp);       // 23.46 C (default 2 decimal places)
    println!("{:.4}", temp);    // 23.4560 C (4 decimal places)
    println!("{:.0}", temp);    // 23 C (no decimal places)
}
```

### Exercise 3: PartialEq Across Types

```rust
#[derive(Debug)]
struct Celsius(f64);

#[derive(Debug)]
struct Fahrenheit(f64);

// TODO: Implement PartialEq<Fahrenheit> for Celsius
// Two temperatures are equal if they represent the same temperature.
// Conversion: C = (F - 32) * 5/9
// Use an epsilon comparison for floating point: (a - b).abs() < 1e-9

// TODO: Implement PartialEq<Celsius> for Fahrenheit (the reverse direction)
// Hint: delegate to the first implementation.

// TODO: Implement PartialEq for Celsius (comparing Celsius to Celsius)
// Also with epsilon comparison.

fn main() {
    let boiling_c = Celsius(100.0);
    let boiling_f = Fahrenheit(212.0);
    let freezing_c = Celsius(0.0);
    let freezing_f = Fahrenheit(32.0);

    println!("100C == 212F? {}", boiling_c == boiling_f);   // true
    println!("212F == 100C? {}", boiling_f == boiling_c);   // true
    println!("0C == 32F? {}", freezing_c == freezing_f);     // true
    println!("100C == 0C? {}", boiling_c == freezing_c);     // false

    // Why can't we implement Eq for Celsius?
    // Because it wraps f64, which doesn't implement Eq (NaN != NaN).
    // Uncomment to see:
    // let nan_c = Celsius(f64::NAN);
    // println!("{}", nan_c == nan_c); // false -- violates Eq's reflexivity
}
```

### Exercise 4: Index for a Custom Collection

```rust
use std::ops::Index;
use std::fmt;

struct Grid<T> {
    data: Vec<T>,
    width: usize,
    height: usize,
}

impl<T: Default + Clone> Grid<T> {
    fn new(width: usize, height: usize) -> Self {
        Grid {
            data: vec![T::default(); width * height],
            width,
            height,
        }
    }

    fn set(&mut self, row: usize, col: usize, value: T) {
        assert!(row < self.height && col < self.width, "index out of bounds");
        self.data[row * self.width + col] = value;
    }
}

// TODO: Implement Index<(usize, usize)> for Grid<T>
// Map (row, col) to the flat index: row * self.width + col
// Panic with a clear message if out of bounds.

// TODO: Implement Index<usize> for Grid<T>
// Index by row number, returning a slice of that row: &[T]
// Hint: &self.data[row * self.width .. (row + 1) * self.width]

// TODO: Implement Display for Grid<T> where T: fmt::Display
// Print as a grid with spaces between elements, newlines between rows.
// Example for a 3x3 grid of i32:
//   1 2 3
//   4 5 6
//   7 8 9

fn main() {
    let mut grid: Grid<i32> = Grid::new(3, 3);

    for row in 0..3 {
        for col in 0..3 {
            grid.set(row, col, (row * 3 + col + 1) as i32);
        }
    }

    // Index by (row, col):
    println!("grid[(0,0)] = {}", grid[(0, 0)]); // 1
    println!("grid[(1,2)] = {}", grid[(1, 2)]); // 6
    println!("grid[(2,2)] = {}", grid[(2, 2)]); // 9

    // Index by row:
    println!("row 1 = {:?}", grid[1usize]); // [4, 5, 6]

    // Display:
    println!("{grid}");
}
```

### Exercise 5: PartialOrd and Custom Sorting

```rust
use std::cmp::Ordering;
use std::fmt;

#[derive(Debug, Clone)]
struct Student {
    name: String,
    grade: f64,  // 0.0 to 100.0
}

impl Student {
    fn new(name: &str, grade: f64) -> Self {
        Student {
            name: name.to_string(),
            grade,
        }
    }
}

impl fmt::Display for Student {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{} ({:.1})", self.name, self.grade)
    }
}

// TODO: Implement PartialEq for Student
// Two students are equal if their grades are equal (epsilon: 1e-9).
// We intentionally compare by grade only for sorting purposes.

// Note: We do NOT implement Eq because grade is f64.

// TODO: Implement PartialOrd for Student
// Higher grades come first (descending order).
// If grades are equal within epsilon, consider them equal.
// Return None if either grade is NaN.
// Hint: self.grade.partial_cmp(&other.grade).map(|o| o.reverse())

fn main() {
    let mut students = vec![
        Student::new("Alice", 92.5),
        Student::new("Bob", 87.3),
        Student::new("Carol", 95.0),
        Student::new("Dave", 87.3),
        Student::new("Eve", 91.0),
    ];

    // We cannot use .sort() because Student doesn't implement Ord.
    // Use .sort_by() with partial_cmp instead:
    students.sort_by(|a, b| {
        a.partial_cmp(b).unwrap_or(Ordering::Equal)
    });

    println!("Sorted by grade (descending):");
    for s in &students {
        println!("  {s}");
    }

    // TODO: Now sort alphabetically by name for students with equal grades.
    // Bob and Dave both have 87.3 — after this sort, Bob should come before Dave.
    // Hint: chain partial_cmp on grade with cmp on name as tiebreaker.
    students.sort_by(|a, b| {
        // TODO: Implement the two-key sort
        todo!()
    });

    println!("\nSorted by grade, then name:");
    for s in &students {
        println!("  {s}");
    }
}
```

## Try It Yourself

1. **Matrix multiplication**: Extend the `Grid` type to implement `Mul<&Grid<f64>> for &Grid<f64>` that performs matrix multiplication. Verify that multiplying a 2x3 matrix by a 3x2 matrix produces a 2x2 matrix.

2. **BitAnd/BitOr for permissions**: Create a `Permissions` struct wrapping a `u8`. Implement `BitAnd`, `BitOr`, and `Not` so you can combine permissions like `READ | WRITE` and check them with `perms & READ == READ`.

3. **Custom Debug**: Implement a `Secret<T>` wrapper where `Debug` prints `Secret(****)` instead of the actual value, but `Display` shows the value normally. This pattern is useful for logging sensitive data.

4. **Total ordering for floats**: Create a wrapper `OrdF64(f64)` that implements `Eq` and `Ord` by treating NaN as greater than all other values. Verify you can sort a `Vec<OrdF64>` with `.sort()`.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Forgetting `type Output` in operator traits | "missing associated type" error | Always specify `type Output` |
| Implementing `Eq` for a type containing `f64` | Violates reflexivity contract (`NaN != NaN`) | Use `PartialEq` only, or wrap in a newtype that handles NaN |
| Making `PartialOrd` inconsistent with `PartialEq` | `a == b` but `a.partial_cmp(b)` is not `Some(Equal)` | Ensure both traits agree on equality |
| Operators that take `self` by value on non-Copy types | Value moved after first use | Derive `Copy` + `Clone`, or implement operators on references |
| Using `{:?}` when you mean `{}` | Gets Debug output instead of Display | Use `{}` for Display, `{:?}` for Debug |
| Assuming `PartialOrd` gives total order | `.sort()` requires `Ord`, not `PartialOrd` | Use `.sort_by()` with a custom comparator |

## Verification

- Exercise 1: All vector operations produce correct results. `2.0 * v` and `v * 2.0` both work.
- Exercise 2: Color prints as `rgb(...)` with `{}`, as `#RRGGBB` with `{:#}`. Temperature respects precision.
- Exercise 3: Cross-type equality works in both directions. Epsilon comparison handles floating-point.
- Exercise 4: `grid[(r,c)]` and `grid[row]` both work. Display shows a formatted grid.
- Exercise 5: Students sort by descending grade, then alphabetically by name for ties.

## Summary

Operators in Rust are trait methods with syntactic sugar. The `std::ops` module maps every operator to a trait, each with an `Output` associated type that controls the result. `Display` gives user-facing formatting, `Debug` gives developer-facing output, and both integrate with Rust's powerful format macro system. The partial-vs-total distinction in `PartialEq`/`Eq` and `PartialOrd`/`Ord` exists because of IEEE 754 floating-point semantics -- NaN breaks reflexivity and total ordering, and Rust's type system makes you deal with that honestly rather than sweeping it under the rug.

## What's Next

- Exercise 13 introduces the newtype pattern, which uses operator overloading heavily to make wrapper types feel like their inner types
- Exercise 14 (builder pattern) and 15 (state machines) apply traits and generics in larger architectural patterns

## Resources

- [std::ops module](https://doc.rust-lang.org/std/ops/index.html)
- [std::fmt module](https://doc.rust-lang.org/std/fmt/index.html)
- [Rust by Example: Operator Overloading](https://doc.rust-lang.org/rust-by-example/trait/ops.html)
- [Rust by Example: Formatting](https://doc.rust-lang.org/rust-by-example/hello/print.html)
- [IEEE 754 and Rust](https://doc.rust-lang.org/std/primitive.f64.html)
