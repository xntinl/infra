# Strings

**Difficulty:** Basico
**Time:** 40-50 minutes
**Prerequisites:** Vectors, ownership, references, slices

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** that `String` is an owned, growable, UTF-8 buffer and `&str` is a borrowed view into one.
- **Understand** why Rust does not allow indexing strings by position, and how to work with characters and byte boundaries safely.
- **Apply** common string methods and ownership patterns to manipulate text in real programs.

## Concepts

### Why strings are hard in Rust

In most languages, `"hello"[1]` gives you `'e'`. In Rust, that line does not compile. The reason is UTF-8. A single visible character like the accented letter "e" can occupy one byte, while a character like a Chinese character can occupy three. Indexing by byte position could land in the middle of a character, producing garbage. Rust refuses to let that happen at compile time rather than silently returning wrong data at runtime.

This feels like friction, but it prevents an entire class of bugs that other languages sweep under the rug.

### String vs &str

These are the two string types you will use constantly:

- `String` -- owned, heap-allocated, growable. It is essentially a `Vec<u8>` that guarantees valid UTF-8. You create it, you can mutate it, and when it goes out of scope it frees its memory.
- `&str` -- a borrowed reference to a sequence of UTF-8 bytes. String literals (`"hello"`) are `&str` with a `'static` lifetime baked into the binary. A `&str` can also point into the middle of a `String`.

The conversion rules:

```rust
let s: String = String::from("hello");
let r: &str = &s;           // String -> &str (cheap, just a reference)
let owned: String = r.to_string(); // &str -> String (allocates)
```

Going from `String` to `&str` is free (it is just borrowing). Going from `&str` to `String` allocates on the heap.

### UTF-8 encoding and slicing

Rust strings are UTF-8 encoded. You can slice them by byte ranges, but the boundaries must fall on character boundaries or the program panics at runtime:

```rust
let s = String::from("hello");
let slice = &s[0..3]; // "hel" -- valid, each char is 1 byte

let emoji = String::from("rust");
// let bad = &emoji[0..1]; // would panic if first char is multi-byte
```

Use `.chars()` to iterate over Unicode scalar values and `.bytes()` to iterate over raw bytes.

### Concatenation

Two common patterns:

```rust
// The + operator: takes ownership of the left side, borrows the right
let a = String::from("hello");
let b = String::from(" world");
let c = a + &b; // a is moved, b is borrowed
// a is no longer valid here

// format! macro: does not take ownership of anything, always allocates
let first = String::from("hello");
let second = String::from("world");
let combined = format!("{} {}", first, second);
// first and second are still valid
```

Use `format!` when you want to keep the original strings. Use `+` when you are done with the left-hand side and want to avoid an extra allocation.

### Common methods

```rust
let mut s = String::from("  Hello, World!  ");

s.push_str(" More text");  // append a &str
s.push('!');                // append a single char

let trimmed = s.trim();    // returns &str, no allocation
let replaced = s.replace("World", "Rust"); // returns new String
let has_hello = s.contains("Hello");       // returns bool

// Splitting
let csv = "a,b,c,d";
let parts: Vec<&str> = csv.split(',').collect();

// Iterating over characters
for ch in "hello".chars() {
    print!("{} ", ch);
}
```

## Exercises

### Exercise 1 -- String vs &str ownership

What do you think this will print? Pay attention to which variables are still valid after each operation:

```rust
fn greet(name: &str) -> String {
    format!("Hello, {}!", name)
}

fn main() {
    // &str -- string literal, lives for the entire program
    let literal: &str = "world";
    println!("{}", greet(literal));
    println!("literal still valid: {}", literal);

    // String -- owned, heap-allocated
    let owned = String::from("Rust");
    println!("{}", greet(&owned)); // pass &String where &str expected (deref coercion)
    println!("owned still valid: {}", owned);

    // Conversion
    let from_literal: String = literal.to_string();
    let back_to_slice: &str = &from_literal;
    println!("from_literal: {}", from_literal);
    println!("back_to_slice: {}", back_to_slice);
}
```

Predict, then verify with `cargo run`.

### Exercise 2 -- UTF-8 and character iteration

Explore why indexing does not work and how to iterate safely:

```rust
fn char_info(s: &str) {
    println!("\"{}\"", s);
    println!("  bytes: {}", s.len());
    println!("  chars: {}", s.chars().count());

    print!("  chars: ");
    for (i, ch) in s.chars().enumerate() {
        if i > 0 { print!(", "); }
        print!("'{}' ({} bytes)", ch, ch.len_utf8());
    }
    println!();
}

fn main() {
    char_info("hello");
    println!();
    char_info("cafe\u{0301}"); // "cafe" + combining accent = "cafe"
    println!();
    char_info("rust");

    // Safe slicing -- ASCII strings where each char is 1 byte
    let ascii = "hello world";
    let word = &ascii[0..5];
    println!("\nSlice of ASCII: \"{}\"", word);

    // nth character (not byte)
    let text = "abcdef";
    if let Some(ch) = text.chars().nth(2) {
        println!("3rd character: '{}'", ch);
    }
}
```

Before running, consider: does "cafe\u{0301}" have 4 characters or 5? (The combining acute accent is a separate Unicode scalar value, so `.chars().count()` returns 5 even though it looks like 4 glyphs.)

### Exercise 3 -- Concatenation patterns

Observe the ownership implications of different concatenation approaches:

```rust
fn main() {
    // + operator: moves the left operand
    let greeting = String::from("Hello");
    let target = String::from("World");
    let message = greeting + ", " + &target + "!";
    println!("{}", message);
    // greeting is moved -- cannot use it
    // target is still valid (only borrowed)
    println!("target still valid: {}", target);

    // format! -- no moves, always allocates a new String
    let first = String::from("Good");
    let second = String::from("Morning");
    let combined = format!("{} {}!", first, second);
    println!("{}", combined);
    println!("first: {}, second: {}", first, second); // both still valid

    // Building up with push_str
    let mut builder = String::new();
    let words = vec!["Rust", "is", "fast"];
    for (i, word) in words.iter().enumerate() {
        if i > 0 {
            builder.push(' ');
        }
        builder.push_str(word);
    }
    println!("{}", builder);
}
```

### Exercise 4 -- Common string methods

Practice the methods you will use daily:

```rust
fn main() {
    let raw = "  Hello, Rust World!  ";

    // trim, to_uppercase, to_lowercase
    println!("trimmed: \"{}\"", raw.trim());
    println!("upper: {}", raw.trim().to_uppercase());
    println!("lower: {}", raw.trim().to_lowercase());

    // contains, starts_with, ends_with
    let url = "https://example.com/api/v2";
    println!("is https: {}", url.starts_with("https"));
    println!("has api: {}", url.contains("/api/"));
    println!("ends v2: {}", url.ends_with("/v2"));

    // replace and replacen
    let template = "Hello, {name}! Welcome to {name}'s dashboard.";
    let filled = template.replace("{name}", "Alice");
    println!("{}", filled);

    let first_only = template.replacen("{name}", "Bob", 1);
    println!("{}", first_only);

    // split and collect
    let csv_line = "Alice,30,Engineer,NYC";
    let fields: Vec<&str> = csv_line.split(',').collect();
    println!("fields: {:?}", fields);
    println!("name: {}, city: {}", fields[0], fields[3]);

    // splitn -- limit the number of splits
    let pair = "key=value=with=equals";
    let parts: Vec<&str> = pair.splitn(2, '=').collect();
    println!("key: {}, value: {}", parts[0], parts[1]);
}
```

Predict each line before running. Pay attention to `replacen` vs `replace` and `splitn` vs `split`.

### Exercise 5 -- Ownership patterns in functions

A common question: should my function take `String` or `&str`? This exercise shows the tradeoffs:

```rust
// Takes &str -- the most flexible, works with both String and &str
fn word_count(text: &str) -> usize {
    text.split_whitespace().count()
}

// Takes String by value -- consumes the input
fn into_slug(title: String) -> String {
    title
        .to_lowercase()
        .split_whitespace()
        .collect::<Vec<&str>>()
        .join("-")
}

// Returns String -- caller owns the result
fn repeat_word(word: &str, times: usize) -> String {
    let mut parts = Vec::with_capacity(times);
    for _ in 0..times {
        parts.push(word);
    }
    parts.join(" ")
}

fn main() {
    // &str parameter accepts both types
    let owned = String::from("the quick brown fox");
    let literal = "jumps over the lazy dog";
    println!("words in owned: {}", word_count(&owned));
    println!("words in literal: {}", word_count(literal));

    // String parameter consumes the input
    let title = String::from("My Blog Post Title");
    let slug = into_slug(title);
    println!("slug: {}", slug);
    // title is no longer valid -- uncomment to see the error:
    // println!("{}", title);

    // If you need to keep the original, clone it
    let title2 = String::from("Another Post");
    let slug2 = into_slug(title2.clone());
    println!("slug: {}, original: {}", slug2, title2);

    // Returning String gives ownership to the caller
    let repeated = repeat_word("ha", 5);
    println!("{}", repeated);
}
```

## Common Mistakes

**Trying to index a string by position:**

```
error[E0277]: the type `str` cannot be indexed by `usize`
  --> src/main.rs:3:20
   |
3  |     let ch = text[0];
   |              ^^^^^^^ string indices are ranges of `usize`
```

Fix: use `.chars().nth(n)` for the nth character, or `&text[start..end]` for a byte-range slice (must be on character boundaries).

**Panicking on invalid byte slice boundaries:**

```
thread 'main' panicked at 'byte index 1 is not a char boundary'
```

Fix: only slice at known-valid boundaries, or use `.char_indices()` to find safe positions.

**Trying to use a String after passing it to a function that takes `String`:**

```
error[E0382]: borrow of moved value: `title`
```

Fix: either clone before passing, or change the function to accept `&str` instead.

## Verification

```bash
# Exercise 1 -- ownership basics
cargo run

# Exercise 2 -- UTF-8 exploration
cargo run

# Exercise 3 -- concatenation
cargo run

# Exercise 4 -- common methods
cargo run

# Exercise 5 -- function signatures
cargo run
```

In Exercise 2, verify that the byte count and character count differ for the Unicode example.

## Summary

- `String` is owned and growable; `&str` is a borrowed view. Prefer `&str` in function parameters.
- Rust strings are UTF-8, so byte count and character count can differ.
- No direct indexing by position -- use `.chars().nth(n)` or byte-range slices.
- `+` moves the left operand; `format!` never moves anything.
- Common methods: `trim`, `contains`, `replace`, `split`, `push_str`, `to_uppercase`.
- When a function takes `String`, it consumes the input. Clone if you need to keep the original.

## What's Next

HashMaps and collections -- where you will combine strings as keys with the collection patterns you have been learning.

## Resources

- [The Rust Book -- Strings](https://doc.rust-lang.org/book/ch08-02-strings.html)
- [std::string::String](https://doc.rust-lang.org/std/string/struct.String.html)
- [Rust By Example -- Strings](https://doc.rust-lang.org/rust-by-example/std/str.html)
- [UTF-8 Everywhere Manifesto](http://utf8everywhere.org/)
