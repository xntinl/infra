# 9. The Slice Type

**Difficulty**: Basico

## Prerequisites
- Exercise 07 (Ownership) completed -- understanding of owned vs borrowed data
- Exercise 08 (References and Borrowing) completed -- understanding of `&T` and `&mut T`
- Familiarity with `String`, `Vec`, and arrays

## Learning Objectives
After completing this exercise, you will be able to:
- Create string slices (`&str`) from `String` values using range syntax
- Explain why string literals are `&str` (slices into the binary)
- Create array slices (`&[T]`) from arrays and vectors
- Write functions that accept slices instead of owned types for maximum flexibility
- Use the full range syntax (`..`, `..=`, `start..`, `..end`)
- Understand why slicing on non-UTF-8 boundaries panics

## Concepts

### What is a Slice?

A slice is a reference to a **contiguous sequence of elements** in a collection. It does not own the data -- it borrows a portion of it. You have already been using slices without realizing it: every time you wrote `&str`, that was a string slice.

Think of a slice as a "fat pointer" -- it stores two pieces of information:
1. A pointer to the first element
2. The length (number of elements)

```
String "hello world"
 index:  0 1 2 3 4 5 6 7 8 9 10

 &s[0..5] = "hello"
   ptr: ----> 'h'
   len: 5

 &s[6..11] = "world"
   ptr: ------------> 'w'
   len: 5
```

### String Slices (&str)

A `&str` is a reference to a sequence of UTF-8 bytes. You create one from a `String` using range indexing.

```rust
fn main() {
    let s = String::from("hello world");

    let hello: &str = &s[0..5];   // bytes 0, 1, 2, 3, 4
    let world: &str = &s[6..11];  // bytes 6, 7, 8, 9, 10

    println!("{hello} {world}");
}
```

Range syntax shortcuts:
- `&s[0..5]` and `&s[..5]` are the same (start defaults to 0)
- `&s[6..11]` and `&s[6..]` are the same if the string has 11 bytes (end defaults to length)
- `&s[0..11]` and `&s[..]` are the same (full slice)

### String Literals Are Slices

When you write `"hello"` in your code, the string data is embedded in the compiled binary. The type of a string literal is `&str` -- a slice pointing into the binary's read-only data section.

```rust
fn main() {
    let literal: &str = "hello"; // points into the compiled binary
    let owned: String = String::from("hello"); // heap-allocated copy

    // Both can be used as &str
    print_str(literal);
    print_str(&owned); // &String coerces to &str automatically
}

fn print_str(s: &str) {
    println!("{s}");
}
```

This is why `&str` is the preferred parameter type for functions that just need to read string data: it accepts both string literals and borrowed `String` values.

### Array Slices (&[T])

The same concept applies to arrays and vectors. `&[T]` is a slice of elements of type `T`.

```rust
fn main() {
    let arr = [10, 20, 30, 40, 50];

    let slice: &[i32] = &arr[1..4]; // elements at index 1, 2, 3
    println!("{:?}", slice); // [20, 30, 40]

    let vec = vec![1, 2, 3, 4, 5];
    let vec_slice: &[i32] = &vec[2..]; // elements from index 2 onward
    println!("{:?}", vec_slice); // [3, 4, 5]
}
```

Just like `&str` is to `String`, `&[T]` is to `Vec<T>` and `[T; N]`. It is a borrowed view into the data without ownership.

### Slices as Function Parameters

A function that takes `&str` is more flexible than one that takes `&String`. A function that takes `&[i32]` is more flexible than one that takes `&Vec<i32>`.

Why? Because `&String` only accepts references to `String` values, but `&str` accepts references to `String`, `&str`, string literals, and slices of strings. The same applies to `&[T]` vs `&Vec<T>`.

```rust
// Restrictive: only accepts &String
fn restricted(s: &String) -> usize {
    s.len()
}

// Flexible: accepts &String, &str, and string literals
fn flexible(s: &str) -> usize {
    s.len()
}
```

Rust automatically coerces `&String` to `&str` and `&Vec<T>` to `&[T]` through a mechanism called "deref coercion." You do not need to do anything special -- just write your functions to accept slices, and pass whatever you have.

### UTF-8 and Slice Boundaries

Rust strings are UTF-8 encoded. Some characters take more than one byte. Slicing operates on **byte indices**, not character indices. If you slice in the middle of a multi-byte character, Rust panics at runtime.

```rust
fn main() {
    let s = String::from("buenos dias"); // all ASCII, each char = 1 byte
    let slice = &s[0..6]; // "buenos" -- OK

    // Multi-byte characters
    let emoji = String::from("hi"); // the emoji takes 4 bytes
    // &emoji[0..3] would panic: byte index 3 is inside the emoji
}
```

This is a deliberate design choice. Rust refuses to silently give you half a character. If you need to work with individual characters, use `.chars()` which iterates by Unicode scalar values.

## Exercises

### Exercise 1: String Slices and Range Syntax

This exercise demonstrates creating slices with different range forms.

Create a new project:

```
$ cargo new slices
$ cd slices
```

Create `src/main.rs`:

```rust
fn main() {
    let sentence = String::from("the quick brown fox");

    // Full range forms
    let the = &sentence[0..3];
    let quick = &sentence[4..9];
    let brown = &sentence[10..15];
    let fox = &sentence[16..19];

    println!("Words: '{the}', '{quick}', '{brown}', '{fox}'");

    // Shorthand forms
    let from_start = &sentence[..9];     // same as [0..9]
    let to_end = &sentence[10..];        // same as [10..19]
    let everything = &sentence[..];      // same as [0..19]

    println!("From start: '{from_start}'");
    println!("To end: '{to_end}'");
    println!("Everything: '{everything}'");

    // Inclusive range
    let inclusive = &sentence[0..=2]; // includes byte at index 2
    println!("Inclusive [0..=2]: '{inclusive}'");
}
```

**What's happening here:**
1. `&sentence[0..3]` creates a `&str` pointing to the first 3 bytes of the `String`.
2. `..9` starts at 0, `10..` goes to the end, `..` takes everything.
3. `0..=2` is an inclusive range -- it includes the element at index 2, giving us bytes 0, 1, and 2.
4. All slices are `&str` -- they borrow from `sentence` without copying any data.

```
$ cargo run
Words: 'the', 'quick', 'brown', 'fox'
From start: 'the quick'
To end: 'brown fox'
Everything: 'the quick brown fox'
Inclusive [0..=2]: 'the'
```

### Exercise 2: Slices as Function Parameters

This exercise shows why `&str` is preferred over `&String` in function signatures.

Replace `src/main.rs`:

```rust
fn first_word(s: &str) -> &str {
    let bytes = s.as_bytes();

    for (i, &byte) in bytes.iter().enumerate() {
        if byte == b' ' {
            return &s[..i];
        }
    }

    s // no space found: the entire string is one word
}

fn word_count(s: &str) -> usize {
    if s.is_empty() {
        return 0;
    }
    s.split_whitespace().count()
}

fn main() {
    // Works with String references
    let owned = String::from("hello world");
    println!("First word of String: '{}'", first_word(&owned));
    println!("Word count: {}", word_count(&owned));

    // Works with string literals directly
    println!("First word of literal: '{}'", first_word("good morning"));
    println!("Word count: {}", word_count("one two three four"));

    // Works with slices of slices
    let slice = &owned[6..]; // "world"
    println!("First word of slice: '{}'", first_word(slice));

    // Edge cases
    println!("Empty: '{}'", first_word(""));
    println!("No spaces: '{}'", first_word("rust"));
    println!("Word count empty: {}", word_count(""));
}
```

**What's happening here:**
1. `first_word` takes `&str`, so it works with `&String`, `&str` literals, and slices of slices.
2. `as_bytes()` gives us raw bytes so we can scan for space characters (`b' '` is the byte value of a space).
3. When we find a space, `&s[..i]` returns a slice from the start up to (not including) the space.
4. If no space is found, the entire input is returned as-is.

```
$ cargo run
First word of String: 'hello'
Word count: 2
First word of literal: 'good'
Word count: 4
First word of slice: 'world'
Empty: ''
No spaces: 'rust'
Word count empty: 0
```

### Exercise 3: Array Slices

This exercise demonstrates slices over arrays and vectors.

Replace `src/main.rs`:

```rust
fn sum(numbers: &[i32]) -> i32 {
    let mut total = 0;
    for &n in numbers {
        total += n;
    }
    total
}

fn find_max(numbers: &[i32]) -> Option<i32> {
    if numbers.is_empty() {
        return None;
    }

    let mut max = numbers[0];
    for &n in &numbers[1..] {
        if n > max {
            max = n;
        }
    }
    Some(max)
}

fn main() {
    // From an array
    let arr = [10, 20, 30, 40, 50];
    println!("Full array sum: {}", sum(&arr));
    println!("First three sum: {}", sum(&arr[..3]));
    println!("Last two sum: {}", sum(&arr[3..]));

    // From a vector
    let vec = vec![5, 15, 25, 35, 45];
    println!("Full vec sum: {}", sum(&vec));
    println!("Middle slice sum: {}", sum(&vec[1..4]));

    // find_max works with any slice
    println!("Max of array: {:?}", find_max(&arr));
    println!("Max of vec slice: {:?}", find_max(&vec[2..]));
    println!("Max of empty: {:?}", find_max(&[]));
}
```

**What's happening here:**
1. `sum` and `find_max` accept `&[i32]` -- a slice of `i32` values. They do not care whether the data comes from an array, a vector, or a sub-slice.
2. `&arr` coerces from `&[i32; 5]` (reference to a fixed-size array) to `&[i32]` (a slice).
3. `&vec` coerces from `&Vec<i32>` to `&[i32]`.
4. `&arr[..3]` creates a sub-slice of the first 3 elements.
5. `&numbers[1..]` inside `find_max` starts from the second element to compare against the initial max.

```
$ cargo run
Full array sum: 150
First three sum: 60
Last two sum: 90
Full vec sum: 125
Middle slice sum: 75
Max of array: Some(50)
Max of vec slice: Some(45)
Max of empty: None
```

### Exercise 4: Mutable Slices

Slices can be mutable too, allowing in-place modification.

Replace `src/main.rs`:

```rust
fn double_all(numbers: &mut [i32]) {
    for n in numbers.iter_mut() {
        *n *= 2;
    }
}

fn zero_negatives(numbers: &mut [i32]) -> usize {
    let mut count = 0;
    for n in numbers.iter_mut() {
        if *n < 0 {
            *n = 0;
            count += 1;
        }
    }
    count
}

fn main() {
    let mut data = [1, 2, 3, 4, 5];
    println!("Before doubling: {:?}", data);

    double_all(&mut data);
    println!("After doubling: {:?}", data);

    // Only double part of the array
    let mut values = [10, 20, 30, 40, 50];
    double_all(&mut values[1..4]); // only doubles indices 1, 2, 3
    println!("Partially doubled: {:?}", values);

    let mut mixed = [5, -3, 8, -1, 0, -7, 4];
    println!("Before zeroing: {:?}", mixed);
    let zeroed = zero_negatives(&mut mixed);
    println!("After zeroing: {:?} ({zeroed} values changed)", mixed);
}
```

**What's happening here:**
1. `&mut [i32]` is a mutable slice -- it lets you modify the elements in place.
2. `iter_mut()` yields mutable references to each element. `*n *= 2` dereferences and modifies.
3. `&mut values[1..4]` creates a mutable slice of just elements at indices 1, 2, and 3.
4. The original array is modified in place -- no new allocation occurs.

```
$ cargo run
Before doubling: [1, 2, 3, 4, 5]
After doubling: [2, 4, 6, 8, 10]
Partially doubled: [10, 40, 60, 80, 50]
Before zeroing: [5, -3, 8, -1, 0, -7, 4]
After zeroing: [5, 0, 8, 0, 0, 0, 4] (3 values changed)
```

### Exercise 5: UTF-8 Boundaries and Safe Slicing

This exercise shows what happens when you slice at invalid UTF-8 boundaries and how to do it safely.

Replace `src/main.rs`:

```rust
fn safe_slice(s: &str, start: usize, end: usize) -> Option<&str> {
    if start > end || end > s.len() {
        return None;
    }
    // Check that both indices fall on character boundaries
    if !s.is_char_boundary(start) || !s.is_char_boundary(end) {
        return None;
    }
    Some(&s[start..end])
}

fn first_n_chars(s: &str, n: usize) -> &str {
    // Find the byte index of the nth character
    match s.char_indices().nth(n) {
        Some((byte_idx, _)) => &s[..byte_idx],
        None => s, // string has fewer than n characters
    }
}

fn main() {
    let ascii = "hello world";
    println!("ASCII slice [0..5]: {:?}", safe_slice(ascii, 0, 5));
    println!("ASCII slice [0..50]: {:?}", safe_slice(ascii, 0, 50));

    let mixed = "cafe\u{0301}"; // "cafe" + combining acute accent = "cafe\u{0301}"
    println!("\nString: {mixed}");
    println!("Byte length: {}", mixed.len());
    println!("Char count: {}", mixed.chars().count());

    // Safe slicing
    println!("safe_slice [0..4]: {:?}", safe_slice(mixed, 0, 4));
    println!("safe_slice [0..5]: {:?}", safe_slice(mixed, 0, 5));

    // Character-based slicing
    let greeting = "Hello";
    println!("\nFirst 3 chars of '{}': '{}'", greeting, first_n_chars(greeting, 3));

    let text = "abcdefgh";
    println!("First 5 chars of '{}': '{}'", text, first_n_chars(text, 5));
    println!("First 100 chars of '{}': '{}'", text, first_n_chars(text, 100));
}
```

**What's happening here:**
1. `safe_slice` checks bounds and UTF-8 character boundaries before slicing. It returns `None` if the slice would be invalid.
2. `is_char_boundary()` returns `true` if the byte index falls at the start of a UTF-8 character (or at the end of the string).
3. `first_n_chars` uses `char_indices()` to find the byte index of the nth character, handling multi-byte characters correctly.
4. `mixed.len()` returns the byte length, while `mixed.chars().count()` returns the character count. These can differ for non-ASCII strings.

What do you think this will print?

```
$ cargo run
ASCII slice [0..5]: Some("hello")
ASCII slice [0..50]: None

String: cafe\u{0301}
Byte length: 5
Char count: 5
safe_slice [0..4]: Some("cafe")
safe_slice [0..5]: Some("cafe\u{0301}")

First 3 chars of 'Hello': 'Hel'
First 5 chars of 'abcdefgh': 'abcde'
First 100 chars of 'abcdefgh': 'abcdefgh'
```

## Common Mistakes

### Slicing at Invalid UTF-8 Boundaries

```rust
fn main() {
    let s = String::from("\u{00e9}toile"); // "etoile" with accented e (2 bytes for e)
    let slice = &s[0..1]; // PANIC: byte 1 is in the middle of the e character
}
```

```
thread 'main' panicked at 'byte index 1 is not a char boundary; it is inside 'e' (bytes 0..2)'
```

The fix: use `is_char_boundary()` to check before slicing, or use `chars()` to work with characters instead of bytes.

### Using &String Instead of &str

```rust
fn greet(name: &String) { // works but unnecessarily restrictive
    println!("Hello, {name}!");
}

fn main() {
    let s = String::from("world");
    greet(&s); // OK
    // greet("world"); // ERROR: &str is not &String
}
```

If you run `cargo clippy`, it will warn you:

```
warning: writing `&String` instead of `&str` involves a new object where a slice will do
```

Fix: use `&str` as the parameter type. It accepts both `&String` (via deref coercion) and `&str`:

```rust
fn greet(name: &str) {
    println!("Hello, {name}!");
}

fn main() {
    let s = String::from("world");
    greet(&s);      // OK: &String coerces to &str
    greet("world"); // OK: &str directly
}
```

### Slicing Beyond Bounds

```rust
fn main() {
    let arr = [1, 2, 3];
    let slice = &arr[0..5]; // PANIC at runtime
}
```

```
thread 'main' panicked at 'range end index 5 out of range for slice of length 3'
```

The compiler cannot always catch out-of-bounds slicing because the indices might be computed at runtime. Use `.get()` for safe access:

```rust
fn main() {
    let arr = [1, 2, 3];
    let safe = arr.get(0..5); // Returns None instead of panicking
    println!("{:?}", safe); // None

    let valid = arr.get(0..2); // Returns Some(&[1, 2])
    println!("{:?}", valid); // Some([1, 2])
}
```

## Verification

```
$ cargo build
   Compiling slices v0.1.0
    Finished `dev` profile

$ cargo run
ASCII slice [0..5]: Some("hello")
ASCII slice [0..50]: None

String: cafe\u{0301}
Byte length: 5
Char count: 5
safe_slice [0..4]: Some("cafe")
safe_slice [0..5]: Some("cafe\u{0301}")

First 3 chars of 'Hello': 'Hel'
First 5 chars of 'abcdefgh': 'abcde'
First 100 chars of 'abcdefgh': 'abcdefgh'

$ cargo clippy
    Finished `dev` profile
```

## Summary

- **Key concepts**: `&str` (string slice), `&[T]` (array/vec slice), range syntax (`..`, `..=`, `start..`, `..end`), slices as fat pointers (pointer + length)
- **What you practiced**: creating slices from Strings, arrays, and vectors; writing functions that accept slices for maximum flexibility; handling UTF-8 boundaries safely; using mutable slices for in-place modification
- **Important to remember**: prefer `&str` over `&String` and `&[T]` over `&Vec<T>` in function parameters; string slicing operates on byte indices, not character indices; slicing at non-character boundaries panics; use `.get()` for bounds-checked access

## What's Next

You now understand ownership, borrowing, and slices -- the core memory model of Rust. Next, you will learn **Structs and Methods**, which let you group related data into custom types and define behavior on them. This is where Rust starts to feel like a powerful systems language rather than just a safe one.

## Resources

- [The Rust Book -- The Slice Type](https://doc.rust-lang.org/book/ch04-03-slices.html)
- [Rust Reference -- Slice types](https://doc.rust-lang.org/reference/types/slice.html)
- [std::primitive::str](https://doc.rust-lang.org/std/primitive.str.html)
- [std::primitive::slice](https://doc.rust-lang.org/std/primitive.slice.html)
