# 17. Iterator Adapters and Custom Iterators

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-basico exercises (closures, ownership, pattern matching)
- Completed: 05-iterators (basic iterator usage, map/filter/collect)
- Completed: 01-traits (trait implementation), 02-generics
- Familiar with `Option<T>`, closures, and method chaining

## Learning Objectives

- Implement the `Iterator` trait for custom types with correct `next()` semantics
- Implement `IntoIterator` to enable for-loop integration
- Chain multiple adapter methods to build complex data pipelines
- Apply `DoubleEndedIterator` for bidirectional traversal
- Create infinite iterators with lazy evaluation
- Evaluate the performance characteristics of iterator chains vs manual loops

## Concepts

### The Iterator Protocol

The `Iterator` trait has one required method:

```rust
trait Iterator {
    type Item;
    fn next(&mut self) -> Option<Self::Item>;
}
```

Every call to `next()` returns `Some(value)` if there is a next element, or `None` when the iterator is exhausted. Once `next()` returns `None`, subsequent calls should also return `None` (this is called "fusing" and is a convention, not enforced by the trait).

The trait provides dozens of free methods built on top of `next()`: `map`, `filter`, `fold`, `collect`, `sum`, `count`, `enumerate`, `zip`, `take`, `skip`, `chain`, `flat_map`, `peekable`, and many more. Implementing `next()` gives you the entire adapter ecosystem for free.

### Lazy Evaluation

Iterator adapters are lazy. Calling `.map(|x| x * 2)` does not compute anything -- it returns a new iterator that will apply the transformation when consumed. Nothing happens until you call a consuming method like `collect()`, `sum()`, `for_each()`, or iterate with a `for` loop:

```rust
let v = vec![1, 2, 3, 4, 5];

// This does NOTHING -- no computation happens:
let lazy = v.iter().map(|x| {
    println!("Processing {x}"); // never printed
    x * 2
});

// This triggers computation:
let result: Vec<_> = lazy.collect(); // now "Processing" is printed 5 times
```

This is the key performance advantage: in a chain like `.filter().map().take(3)`, only the elements that survive the filter and fit within the first 3 are ever processed. The rest are never touched.

### Key Adapters Reference

```rust
let data = vec![1, 2, 3, 4, 5, 6, 7, 8, 9, 10];

// map: transform each element
data.iter().map(|x| x * 2);       // [2, 4, 6, 8, ...]

// filter: keep elements matching a predicate
data.iter().filter(|x| **x > 5);  // [6, 7, 8, 9, 10]

// enumerate: attach indices
data.iter().enumerate();           // [(0,1), (1,2), (2,3), ...]

// zip: combine two iterators
data.iter().zip(data.iter().rev()); // [(1,10), (2,9), (3,8), ...]

// take / skip: limit the iterator
data.iter().take(3);               // [1, 2, 3]
data.iter().skip(7);               // [8, 9, 10]

// flat_map: map + flatten
vec![vec![1,2], vec![3,4]].iter()
    .flat_map(|v| v.iter());       // [1, 2, 3, 4]

// chain: concatenate iterators
data[..3].iter().chain(data[7..].iter()); // [1, 2, 3, 8, 9, 10]

// peekable: look at next without consuming
let mut peek = data.iter().peekable();
peek.peek();  // Some(&1), does not advance

// collect: gather into a collection
data.iter().filter(|x| **x > 5).collect::<Vec<_>>();
```

### `IntoIterator` and the For Loop

The `for` loop desugars into `IntoIterator`:

```rust
// This:
for item in collection {
    // ...
}

// Is equivalent to:
let mut iter = collection.into_iter();
while let Some(item) = iter.next() {
    // ...
}
```

There are three common `IntoIterator` implementations for collections:

```rust
// Consuming: for item in vec        -> IntoIterator for Vec<T>,       Item = T
// Shared ref: for item in &vec      -> IntoIterator for &Vec<T>,      Item = &T
// Mutable ref: for item in &mut vec -> IntoIterator for &mut Vec<T>,  Item = &mut T
```

When you implement a custom collection, providing all three is the ergonomic standard.

### `DoubleEndedIterator`

`DoubleEndedIterator` adds `next_back()`, allowing iteration from both ends:

```rust
trait DoubleEndedIterator: Iterator {
    fn next_back(&mut self) -> Option<Self::Item>;
}
```

This enables `.rev()` (which swaps `next` and `next_back`) and methods like `rfold` and `rfind`. Slices, `Vec`, `VecDeque`, and ranges all implement it.

### Infinite Iterators

An iterator does not have to end. `std::iter::repeat`, `std::iter::from_fn`, and custom implementations can produce values forever:

```rust
// Built-in infinite iterators:
let ones = std::iter::repeat(1);         // 1, 1, 1, 1, ...
let counter = (0..).into_iter();         // 0, 1, 2, 3, ...
let fibs = std::iter::successors(
    Some((0u64, 1u64)),
    |&(a, b)| Some((b, a + b)),
).map(|(a, _)| a);                       // 0, 1, 1, 2, 3, 5, 8, ...

// DANGER: Calling .collect() on an infinite iterator never terminates.
// Always use .take(n) before collecting:
let first_10: Vec<_> = (0..).take(10).collect();
```

### Performance: Iterator Chains vs Manual Loops

Iterator chains compile down to the same machine code as hand-written loops. The Rust compiler inlines and optimizes adapter chains aggressively. In many cases, iterators are faster than indexing because they avoid bounds checks:

```rust
// These produce identical assembly (zero-cost abstraction):

// Iterator chain:
let sum: i32 = data.iter()
    .filter(|x| **x % 2 == 0)
    .map(|x| x * 3)
    .sum();

// Manual loop:
let mut sum = 0i32;
for x in &data {
    if x % 2 == 0 {
        sum += x * 3;
    }
}
```

The iterator version is often preferred because it is more composable and harder to introduce off-by-one errors.

### Anti-Pattern: Collecting Intermediate Results

```rust
// WRONG: Allocates an unnecessary Vec
let filtered: Vec<_> = data.iter().filter(|x| **x > 5).collect();
let sum: i32 = filtered.iter().sum();

// RIGHT: Chain directly
let sum: i32 = data.iter().filter(|x| **x > 5).sum();
```

Each `.collect()` allocates a new `Vec`. Chaining adapters avoids intermediate allocations.

## Exercises

### Exercise 1: Implement a Range Iterator

Build a custom `Range` struct that iterates over integers from `start` to `end` (exclusive).

```rust
/// A custom range iterator that yields integers from start (inclusive)
/// to end (exclusive).
struct Range {
    current: i64,
    end: i64,
}

impl Range {
    fn new(start: i64, end: i64) -> Self {
        Range { current: start, end }
    }
}

// TODO: Implement Iterator for Range.
// type Item = i64
// next() should:
//   - If current < end: save current, increment it, return Some(saved)
//   - Otherwise: return None
//
// impl Iterator for Range {
//     type Item = i64;
//     fn next(&mut self) -> Option<Self::Item> {
//         todo!()
//     }
// }

// TODO: Implement DoubleEndedIterator for Range.
// next_back() should:
//   - If current < end: decrement end, return Some(end)
//   - Otherwise: return None
// This mirrors how std::ops::Range works from both ends.
//
// impl DoubleEndedIterator for Range {
//     fn next_back(&mut self) -> Option<Self::Item> {
//         todo!()
//     }
// }

// TODO: Implement a `step_by_custom` method that returns a SteppedRange
// iterator yielding every Nth value.
//
// struct SteppedRange {
//     current: i64,
//     end: i64,
//     step: i64,
// }
//
// impl Range {
//     fn step_by_custom(self, step: i64) -> SteppedRange {
//         todo!()
//     }
// }
//
// impl Iterator for SteppedRange {
//     type Item = i64;
//     fn next(&mut self) -> Option<Self::Item> {
//         todo!()
//     }
// }

fn main() {
    // Basic usage:
    let range = Range::new(1, 6);
    let values: Vec<i64> = range.collect();
    println!("Range: {values:?}"); // [1, 2, 3, 4, 5]

    // With adapters:
    let evens: Vec<i64> = Range::new(1, 11)
        .filter(|x| x % 2 == 0)
        .collect();
    println!("Evens: {evens:?}"); // [2, 4, 6, 8, 10]

    let sum: i64 = Range::new(1, 101).sum();
    println!("Sum 1..100: {sum}"); // 5050

    // DoubleEndedIterator:
    let reversed: Vec<i64> = Range::new(1, 6).rev().collect();
    println!("Reversed: {reversed:?}"); // [5, 4, 3, 2, 1]

    // Consuming from both ends:
    let mut range = Range::new(1, 6);
    println!("Front: {:?}", range.next());      // Some(1)
    println!("Back:  {:?}", range.next_back());  // Some(5)
    println!("Front: {:?}", range.next());      // Some(2)
    println!("Back:  {:?}", range.next_back());  // Some(4)
    println!("Front: {:?}", range.next());      // Some(3)
    println!("Front: {:?}", range.next());      // None

    // Stepped range:
    let stepped: Vec<i64> = Range::new(0, 20).step_by_custom(3).collect();
    println!("Step 3: {stepped:?}"); // [0, 3, 6, 9, 12, 15, 18]
}
```

### Exercise 2: Fibonacci Iterator

Build an infinite Fibonacci iterator.

```rust
/// An infinite iterator that yields Fibonacci numbers: 0, 1, 1, 2, 3, 5, 8, ...
struct Fibonacci {
    a: u64,
    b: u64,
}

impl Fibonacci {
    fn new() -> Self {
        Fibonacci { a: 0, b: 1 }
    }
}

// TODO: Implement Iterator for Fibonacci.
// type Item = u64
// next() should:
//   1. Save the current value of `a`
//   2. Update: new_a = b, new_b = a + b
//   3. Return Some(saved)
//
// impl Iterator for Fibonacci {
//     type Item = u64;
//     fn next(&mut self) -> Option<Self::Item> {
//         todo!()
//     }
// }

/// An iterator that generates the Collatz sequence starting from n.
/// Rule: if n is even, next = n/2. If n is odd, next = 3n+1. Stops at 1.
struct Collatz {
    current: Option<u64>,
}

impl Collatz {
    fn new(start: u64) -> Self {
        Collatz { current: Some(start) }
    }
}

// TODO: Implement Iterator for Collatz.
// type Item = u64
// next() should:
//   1. Take the current value (use self.current.take()?)
//   2. If it was Some(1), set self.current to None, return Some(1)
//   3. If it was Some(n):
//      - if even: self.current = Some(n / 2)
//      - if odd:  self.current = Some(3 * n + 1)
//      - return Some(n)
//   4. If None, return None
//
// impl Iterator for Collatz {
//     type Item = u64;
//     fn next(&mut self) -> Option<Self::Item> {
//         todo!()
//     }
// }

fn main() {
    // First 10 Fibonacci numbers:
    let fibs: Vec<u64> = Fibonacci::new().take(10).collect();
    println!("First 10 Fibonacci: {fibs:?}");
    // [0, 1, 1, 2, 3, 5, 8, 13, 21, 34]

    // Find the first Fibonacci number greater than 1000:
    let big = Fibonacci::new().find(|&x| x > 1000);
    println!("First Fib > 1000: {big:?}"); // Some(1597)

    // Sum of Fibonacci numbers below 100:
    let sum: u64 = Fibonacci::new().take_while(|&x| x < 100).sum();
    println!("Sum of Fibs < 100: {sum}"); // 232

    // Count Fibonacci numbers below 1_000_000:
    let count = Fibonacci::new().take_while(|&x| x < 1_000_000).count();
    println!("Fibs below 1M: {count}"); // 30

    // Enumerate with index:
    println!("\nFib with indices:");
    for (i, fib) in Fibonacci::new().take(8).enumerate() {
        println!("  F({i}) = {fib}");
    }

    // Zip Fibonacci with its own shifted version to get ratios:
    println!("\nGolden ratio approximation:");
    let ratios: Vec<f64> = Fibonacci::new()
        .skip(1)  // skip 0 to avoid division by zero
        .zip(Fibonacci::new().skip(2))
        .take(10)
        .map(|(a, b)| b as f64 / a as f64)
        .collect();
    for (i, ratio) in ratios.iter().enumerate() {
        println!("  F({})/F({}) = {:.6}", i + 2, i + 1, ratio);
    }

    // Collatz sequence:
    let seq: Vec<u64> = Collatz::new(27).collect();
    println!("\nCollatz(27): length = {}", seq.len());
    println!("First 10: {:?}", &seq[..10]);
    println!("Last 5: {:?}", &seq[seq.len()-5..]);

    // Find the starting number (1..100) with the longest Collatz sequence:
    let (longest_start, longest_len) = (1u64..100)
        .map(|n| (n, Collatz::new(n).count()))
        .max_by_key(|&(_, len)| len)
        .unwrap();
    println!("Longest Collatz under 100: start={longest_start}, length={longest_len}");
}
```

### Exercise 3: Grid Iterator with Flat Mapping

Build a 2D grid and implement iteration over its cells.

```rust
use std::fmt;

#[derive(Debug, Clone)]
struct Grid<T> {
    data: Vec<Vec<T>>,
    rows: usize,
    cols: usize,
}

impl<T: Clone + Default> Grid<T> {
    fn new(rows: usize, cols: usize) -> Self {
        Grid {
            data: vec![vec![T::default(); cols]; rows],
            rows,
            cols,
        }
    }

    fn get(&self, row: usize, col: usize) -> Option<&T> {
        self.data.get(row)?.get(col)
    }

    fn set(&mut self, row: usize, col: usize, value: T) {
        if row < self.rows && col < self.cols {
            self.data[row][col] = value;
        }
    }
}

/// An iterator over grid cells yielding (row, col, &value) tuples.
struct GridIter<'a, T> {
    grid: &'a Grid<T>,
    row: usize,
    col: usize,
}

// TODO: Implement Iterator for GridIter.
// type Item = (usize, usize, &'a T)
// next() should:
//   1. If row >= grid.rows, return None (exhausted)
//   2. Get a reference to grid.data[row][col]
//   3. Save the current (row, col)
//   4. Advance: col += 1. If col >= grid.cols, reset col = 0 and row += 1.
//   5. Return Some((saved_row, saved_col, &value))
//
// impl<'a, T> Iterator for GridIter<'a, T> {
//     type Item = (usize, usize, &'a T);
//     fn next(&mut self) -> Option<Self::Item> {
//         todo!()
//     }
// }

impl<T> Grid<T> {
    // TODO: Implement iter() that returns a GridIter.
    //
    // fn iter(&self) -> GridIter<'_, T> {
    //     todo!()
    // }

    // TODO: Implement row_iter that returns an iterator over a single row.
    // Hint: Return self.data[row].iter() wrapped in an Option or use
    // self.data.get(row).map(|r| r.iter()) and flatten.
    //
    // fn row_iter(&self, row: usize) -> impl Iterator<Item = &T> {
    //     todo!()
    // }

    // TODO: Implement col_iter that returns an iterator over a single column.
    // Hint: Iterate over rows, picking column `col` from each.
    //
    // fn col_iter(&self, col: usize) -> impl Iterator<Item = &T> {
    //     todo!()
    // }
}

// TODO: Implement IntoIterator for &Grid<T> so for-loops work.
//
// impl<'a, T> IntoIterator for &'a Grid<T> {
//     type Item = (usize, usize, &'a T);
//     type IntoIter = GridIter<'a, T>;
//     fn into_iter(self) -> Self::IntoIter {
//         self.iter()
//     }
// }

impl<T: fmt::Display> fmt::Display for Grid<T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        for row in &self.data {
            for (i, val) in row.iter().enumerate() {
                if i > 0 {
                    write!(f, "\t")?;
                }
                write!(f, "{val}")?;
            }
            writeln!(f)?;
        }
        Ok(())
    }
}

fn main() {
    let mut grid: Grid<i32> = Grid::new(3, 4);

    // Fill with values:
    for row in 0..3 {
        for col in 0..4 {
            grid.set(row, col, (row * 4 + col + 1) as i32);
        }
    }

    println!("Grid:");
    println!("{grid}");
    // 1    2    3    4
    // 5    6    7    8
    // 9    10   11   12

    // Iterate with (row, col, value):
    println!("All cells:");
    for (r, c, val) in &grid {
        println!("  ({r}, {c}) = {val}");
    }

    // Sum all elements:
    let sum: i32 = grid.iter().map(|(_, _, val)| val).sum();
    println!("Sum: {sum}"); // 78

    // Find max:
    let max = grid.iter().map(|(_, _, val)| val).max();
    println!("Max: {max:?}"); // Some(12)

    // Filter even values with their positions:
    let evens: Vec<_> = grid.iter()
        .filter(|(_, _, val)| **val % 2 == 0)
        .map(|(r, c, val)| (r, c, *val))
        .collect();
    println!("Evens: {evens:?}");

    // Row iteration:
    print!("Row 1: ");
    for val in grid.row_iter(1) {
        print!("{val} ");
    }
    println!(); // 5 6 7 8

    // Column iteration:
    print!("Col 2: ");
    for val in grid.col_iter(2) {
        print!("{val} ");
    }
    println!(); // 3 7 11

    // Flat_map example: get all values from all rows:
    let flat: Vec<&i32> = grid.data.iter()
        .flat_map(|row| row.iter())
        .collect();
    println!("Flat: {flat:?}");

    // Zip two columns together:
    let pairs: Vec<_> = grid.col_iter(0)
        .zip(grid.col_iter(3))
        .collect();
    println!("Col 0 + Col 3 pairs: {pairs:?}"); // [(&1, &4), (&5, &8), (&9, &12)]
}
```

### Exercise 4: Peekable and Windows

Use `peekable()` to implement a tokenizer and explore sliding window patterns.

```rust
#[derive(Debug, PartialEq)]
enum Token {
    Number(f64),
    Plus,
    Minus,
    Star,
    Slash,
    LeftParen,
    RightParen,
    Unknown(char),
}

/// A simple tokenizer that uses Peekable to look ahead.
struct Tokenizer<I: Iterator<Item = char>> {
    chars: std::iter::Peekable<I>,
}

impl<I: Iterator<Item = char>> Tokenizer<I> {
    fn new(iter: I) -> Self {
        Tokenizer {
            chars: iter.peekable(),
        }
    }

    /// Skip whitespace by advancing past space characters.
    fn skip_whitespace(&mut self) {
        while let Some(&ch) = self.chars.peek() {
            if ch.is_whitespace() {
                self.chars.next();
            } else {
                break;
            }
        }
    }

    // TODO: Implement read_number that reads a full number (possibly with decimals).
    // Use peek() to check if the next char is a digit or '.', and next() to consume.
    // Collect digits into a String, then parse to f64.
    //
    // fn read_number(&mut self) -> f64 {
    //     let mut num_str = String::new();
    //     while let Some(&ch) = self.chars.peek() {
    //         if ch.is_ascii_digit() || ch == '.' {
    //             todo!() // consume the char and add to num_str
    //         } else {
    //             break;
    //         }
    //     }
    //     num_str.parse().unwrap_or(0.0)
    // }
}

// TODO: Implement Iterator for Tokenizer.
// type Item = Token
// next() should:
//   1. skip_whitespace()
//   2. peek at the next char
//   3. If it's a digit or '.', call read_number() and return Token::Number
//   4. Otherwise consume it with self.chars.next() and match:
//      '+' => Token::Plus
//      '-' => Token::Minus
//      '*' => Token::Star
//      '/' => Token::Slash
//      '(' => Token::LeftParen
//      ')' => Token::RightParen
//      other => Token::Unknown(other)
//   5. If peek returns None, return None
//
// impl<I: Iterator<Item = char>> Iterator for Tokenizer<I> {
//     type Item = Token;
//     fn next(&mut self) -> Option<Self::Item> {
//         todo!()
//     }
// }

/// Sliding window: compute a moving average using windows().
fn moving_average(data: &[f64], window_size: usize) -> Vec<f64> {
    // TODO: Use data.windows(window_size) to compute the moving average.
    // windows() yields slices of length window_size.
    // For each window, compute the average.
    //
    // data.windows(window_size)
    //     .map(|window| todo!())
    //     .collect()
    todo!()
}

/// Detect runs: find consecutive groups of equal elements.
/// Returns a Vec of (value, count) pairs.
fn run_length_encode<T: PartialEq + Clone>(data: &[T]) -> Vec<(T, usize)> {
    // TODO: Use a Peekable iterator to group consecutive equal elements.
    // Algorithm:
    //   1. Create a peekable iterator over data
    //   2. For each element, count how many consecutive equal elements follow
    //   3. Use peek() to check if the next element is the same
    //
    let mut result = Vec::new();
    let mut iter = data.iter().peekable();

    while let Some(current) = iter.next() {
        let mut count = 1;
        // TODO: While the next element equals current, consume it and increment count
        // while let Some(&next) = iter.peek() {
        //     todo!()
        // }
        result.push((current.clone(), count));
    }

    result
}

fn main() {
    // Tokenizer:
    let input = "3.14 + (42 - 7) * 2 / 1.5";
    let tokens: Vec<Token> = Tokenizer::new(input.chars()).collect();
    println!("Tokens:");
    for token in &tokens {
        println!("  {token:?}");
    }
    // Token::Number(3.14), Token::Plus, Token::LeftParen, Token::Number(42),
    // Token::Minus, Token::Number(7), Token::RightParen, Token::Star,
    // Token::Number(2), Token::Slash, Token::Number(1.5)

    assert_eq!(tokens[0], Token::Number(3.14));
    assert_eq!(tokens[1], Token::Plus);
    assert_eq!(tokens[2], Token::LeftParen);
    assert_eq!(tokens[3], Token::Number(42.0));

    // Moving average:
    let prices = vec![10.0, 11.0, 12.0, 11.5, 13.0, 14.0, 13.5, 15.0];
    let ma3 = moving_average(&prices, 3);
    println!("\nPrices: {prices:?}");
    println!("MA(3):  {ma3:.2?}");
    // [11.0, 11.5, 12.17, 12.83, 13.5, 14.17]

    // Run-length encoding:
    let data = vec!['a', 'a', 'a', 'b', 'b', 'c', 'a', 'a'];
    let encoded = run_length_encode(&data);
    println!("\nRLE: {encoded:?}");
    // [('a', 3), ('b', 2), ('c', 1), ('a', 2)]

    let numbers = vec![1, 1, 1, 2, 2, 3, 3, 3, 3, 1];
    let encoded = run_length_encode(&numbers);
    println!("RLE: {encoded:?}");
    // [(1, 3), (2, 2), (3, 4), (1, 1)]

    // Other peekable uses -- pair adjacent elements:
    let vals = vec![1, 2, 3, 4, 5];
    let mut iter = vals.iter().peekable();
    let mut pairs = Vec::new();
    while let Some(&current) = iter.next() {
        if let Some(&&next) = iter.peek() {
            pairs.push((current, next));
        }
    }
    println!("\nAdjacent pairs: {pairs:?}");
    // [(1, 2), (2, 3), (3, 4), (4, 5)]
}
```

### Exercise 5: Composing Adapter Chains -- Data Pipeline

Build a data processing pipeline using adapter chains. No custom iterators here; this exercise focuses on fluency with standard adapters.

```rust
use std::collections::HashMap;

#[derive(Debug, Clone)]
struct LogEntry {
    timestamp: String,
    level: String,
    message: String,
    response_time_ms: Option<u64>,
}

impl LogEntry {
    fn new(ts: &str, level: &str, msg: &str, rt: Option<u64>) -> Self {
        LogEntry {
            timestamp: ts.to_string(),
            level: level.to_string(),
            message: msg.to_string(),
            response_time_ms: rt,
        }
    }
}

fn sample_logs() -> Vec<LogEntry> {
    vec![
        LogEntry::new("2026-03-13T10:00:01", "INFO",  "Request GET /api/users", Some(45)),
        LogEntry::new("2026-03-13T10:00:02", "DEBUG", "Cache hit for user_123", None),
        LogEntry::new("2026-03-13T10:00:03", "INFO",  "Request POST /api/orders", Some(230)),
        LogEntry::new("2026-03-13T10:00:04", "WARN",  "Slow query on orders table", Some(1520)),
        LogEntry::new("2026-03-13T10:00:05", "ERROR", "Connection timeout to DB", Some(5000)),
        LogEntry::new("2026-03-13T10:00:06", "INFO",  "Request GET /api/users", Some(38)),
        LogEntry::new("2026-03-13T10:00:07", "INFO",  "Request GET /api/products", Some(95)),
        LogEntry::new("2026-03-13T10:00:08", "WARN",  "Rate limit approaching", None),
        LogEntry::new("2026-03-13T10:00:09", "INFO",  "Request POST /api/orders", Some(189)),
        LogEntry::new("2026-03-13T10:00:10", "ERROR", "Failed to process payment", Some(3200)),
        LogEntry::new("2026-03-13T10:00:11", "INFO",  "Request GET /api/users", Some(42)),
        LogEntry::new("2026-03-13T10:00:12", "DEBUG", "Metrics flush", None),
        LogEntry::new("2026-03-13T10:00:13", "INFO",  "Request DELETE /api/orders/5", Some(67)),
        LogEntry::new("2026-03-13T10:00:14", "INFO",  "Request GET /api/products", Some(102)),
        LogEntry::new("2026-03-13T10:00:15", "WARN",  "Memory usage at 85%", None),
    ]
}

fn main() {
    let logs = sample_logs();

    // TODO 1: Count logs by level.
    // Use .iter().fold() with a HashMap<&str, usize> to count occurrences of each level.
    //
    // let level_counts: HashMap<&str, usize> = logs.iter()
    //     .fold(HashMap::new(), |mut acc, entry| {
    //         todo!() // increment the count for entry.level
    //         acc
    //     });
    // println!("Counts by level: {level_counts:?}");
    // Expected: {"INFO": 7, "DEBUG": 2, "WARN": 3, "ERROR": 2, ...}

    // TODO 2: Get the average response time for INFO-level requests.
    // Chain: filter (INFO only) -> filter_map (extract response_time_ms) -> collect to Vec
    // Then compute average.
    //
    // let info_times: Vec<u64> = logs.iter()
    //     .filter(|e| todo!())        // keep only INFO
    //     .filter_map(|e| todo!())    // extract response_time_ms (skip None)
    //     .collect();
    // let avg = info_times.iter().sum::<u64>() as f64 / info_times.len() as f64;
    // println!("Avg INFO response time: {avg:.1}ms");

    // TODO 3: Find the top 3 slowest requests.
    // Chain: filter_map (keep entries with response times) -> collect to Vec
    // Sort by response time descending, take first 3.
    //
    // let mut with_times: Vec<_> = logs.iter()
    //     .filter_map(|e| {
    //         todo!() // return Some((message, time)) if response_time_ms is Some
    //     })
    //     .collect();
    // with_times.sort_by(|a, b| todo!()); // sort descending by time
    // println!("\nTop 3 slowest:");
    // for (msg, time) in with_times.iter().take(3) {
    //     println!("  {time}ms - {msg}");
    // }

    // TODO 4: Group log messages by extracting the HTTP method and path.
    // For entries whose message starts with "Request ", extract "GET /api/users" etc.
    // Use filter_map + fold to build a HashMap<String, usize> counting requests per endpoint.
    //
    // let endpoint_counts: HashMap<String, usize> = logs.iter()
    //     .filter_map(|e| {
    //         e.message.strip_prefix("Request ")
    //             .map(|rest| rest.to_string())
    //     })
    //     .fold(HashMap::new(), |mut acc, endpoint| {
    //         todo!() // increment count
    //         acc
    //     });
    // println!("\nRequests per endpoint:");
    // for (endpoint, count) in &endpoint_counts {
    //     println!("  {endpoint}: {count}");
    // }

    // TODO 5: Build a summary report using enumerate and chain.
    // Create one string per log line: "{index}: [{level}] {message}"
    // Then chain a header and footer:
    //
    // let header = std::iter::once("=== LOG REPORT ===".to_string());
    // let footer = std::iter::once(format!("=== {} entries total ===", logs.len()));
    //
    // let report_lines: Vec<String> = header
    //     .chain(
    //         logs.iter()
    //             .enumerate()
    //             .map(|(i, e)| todo!()) // format each line
    //     )
    //     .chain(footer)
    //     .collect();
    //
    // println!("\nReport:");
    // for line in &report_lines {
    //     println!("{line}");
    // }
}
```

## Try It Yourself

1. **Consuming iterator**: Add an `into_iter()` method to the `Grid` from Exercise 3 that consumes the grid and yields owned values. Implement `IntoIterator` for `Grid<T>` (not `&Grid<T>`).

2. **Cartesian product**: Write a function that takes two iterators and returns an iterator over all pairs `(a, b)`. Hint: collect the second into a Vec, then use `flat_map` over the first, cloning the Vec for each element.

3. **Interleave**: Write a struct `Interleave<A, B>` that alternates between two iterators: first yields from A, then B, then A, then B, and so on. When one is exhausted, continue with the other.

4. **Benchmark**: Create a large Vec of 1 million random numbers. Compare the time to compute a sum-of-squares using (a) an iterator chain `.iter().map(|x| x*x).sum()`, (b) a manual for-loop, and (c) `.iter().map(|x| x*x).collect::<Vec<_>>()` followed by `.iter().sum()`. Observe that (a) and (b) are fast while (c) is slower due to the intermediate allocation.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Calling `collect()` too early in a chain | Unnecessary allocation, loss of laziness | Keep adapters lazy; collect only at the end |
| Forgetting that `filter` receives `&&T` for iter() | Type errors with double references | Use `**x` to dereference, or `filter(\|&&x\| ...)` |
| Calling `.collect()` on an infinite iterator | Program hangs forever | Always use `.take(n)` before collecting infinite iterators |
| Returning an iterator from a function without `impl Iterator` | Cannot name the complex adapter type | Use `-> impl Iterator<Item = T>` as the return type |
| Mutating state inside `map()` | Surprising behavior, order-dependent bugs | Use `for_each()` for side effects, `map()` for pure transforms |
| Implementing `next()` without advancing state | Infinite loop when consumed | Always modify `self` to make progress toward `None` |

## Verification

```bash
# Exercise 1: Range iterator
rustc exercises/ex1_range.rs && ./ex1_range
# Expected: Range [1..5], evens, sum=5050, reversed, stepped

# Exercise 2: Fibonacci and Collatz
rustc exercises/ex2_fibonacci.rs && ./ex2_fibonacci
# Expected: [0,1,1,2,3,5,8,13,21,34], golden ratio converging to 1.618...

# Exercise 3: Grid iterator
rustc exercises/ex3_grid.rs && ./ex3_grid
# Expected: Grid display, cell iteration, row/col iteration, flat_map

# Exercise 4: Peekable tokenizer
rustc exercises/ex4_peekable.rs && ./ex4_peekable
# Expected: Tokens parsed, moving average, run-length encoding

# Exercise 5: Data pipeline
rustc exercises/ex5_pipeline.rs && ./ex5_pipeline
# Expected: Level counts, avg response time, top 3 slowest, endpoint counts
```

## Summary

Custom iterators in Rust are built by implementing the `Iterator` trait with a single `next()` method. This gives you access to the entire adapter ecosystem for free: `map`, `filter`, `fold`, `zip`, `enumerate`, `take`, `chain`, `flat_map`, `peekable`, and dozens more. Adapters are lazy -- they do no work until consumed -- which enables both composability and performance. `IntoIterator` integrates your types with `for` loops. `DoubleEndedIterator` adds reverse traversal. Infinite iterators are safe as long as you bound them with `take()` or `take_while()`. The performance of adapter chains matches hand-written loops because the compiler inlines everything, making iterators a true zero-cost abstraction.

## What You Learned

- How to implement `Iterator` and `IntoIterator` for custom types
- Building `DoubleEndedIterator` for bidirectional iteration
- Creating infinite iterators (Fibonacci, Collatz) with correct lazy semantics
- Using `peekable()` for lookahead in tokenizers and grouping algorithms
- Chaining adapters into complex data pipelines with no intermediate allocations
- The `windows()` method for sliding-window computations
- Why iterator chains and manual loops produce the same machine code

## Resources

- [The Rust Book: Processing a Series of Items with Iterators](https://doc.rust-lang.org/book/ch13-02-iterators.html)
- [Iterator trait documentation](https://doc.rust-lang.org/std/iter/trait.Iterator.html)
- [DoubleEndedIterator documentation](https://doc.rust-lang.org/std/iter/trait.DoubleEndedIterator.html)
- [IntoIterator documentation](https://doc.rust-lang.org/std/iter/trait.IntoIterator.html)
- [Rust by Example: Iterators](https://doc.rust-lang.org/rust-by-example/trait/iter.html)
- [Blog: Rust's Iterators are More Than Just For Loops](https://blog.jetbrains.com/rust/2024/09/17/rusts-iterators/)
