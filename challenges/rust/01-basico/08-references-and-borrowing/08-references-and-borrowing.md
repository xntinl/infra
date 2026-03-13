# 8. References and Borrowing

**Difficulty**: Basico

## Prerequisites
- Exercise 07 (Ownership) completed -- you must understand move semantics, `Copy`, and `Clone`
- Understanding of stack vs heap, the three ownership rules
- Familiarity with `String` vs `&str`

## Learning Objectives
After completing this exercise, you will be able to:
- Create immutable references (`&T`) to borrow values without taking ownership
- Create mutable references (`&mut T`) to modify borrowed values
- Apply the borrowing rules: multiple `&T` OR one `&mut T`, never both
- Explain how Rust prevents dangling references at compile time
- Understand Non-Lexical Lifetimes (NLL) and reference scopes

## Concepts

### Why Borrowing Exists

In the previous exercise, you saw this pattern:

```rust
fn calculate_length(s: String) -> (String, usize) {
    let len = s.len();
    (s, len) // have to return s to give ownership back
}
```

This works, but it is awkward. Every function that needs to read a value would have to take ownership and then return it. References solve this: they let a function **borrow** a value without taking ownership.

### Immutable References (&T)

A reference is like a pointer that is guaranteed to be valid. The `&` symbol creates a reference, and `*` dereferences it (though Rust auto-dereferences in most cases, so you rarely write `*` explicitly).

```rust
fn calculate_length(s: &String) -> usize {
    s.len() // s is a reference, but we can call methods on it directly
}

fn main() {
    let s1 = String::from("hello");
    let len = calculate_length(&s1); // &s1 creates a reference to s1

    // s1 is still valid -- ownership was NOT transferred
    println!("The length of '{s1}' is {len}");
}
```

```
Stack                           Heap
+------------------+            +----------------------------+
| main:            |            |                            |
|   s1: ptr -------+----------->| "hello" (5 bytes)          |
|       len: 5     |            |                            |
|       cap: 5     |            +----------------------------+
|                  |                      ^
| calculate_length:|                      |
|   s: &-----------+----- points to s1's stack data
+------------------+        (which points to the heap)
```

The reference `s` in `calculate_length` borrows `s1`. When `calculate_length` returns, the reference is gone, but `s1` is unchanged. No ownership was transferred.

We call this "borrowing" -- like borrowing a book. You can read it, but you do not own it, and you must give it back (the reference goes away when the function ends).

### Mutable References (&mut T)

Immutable references let you read. Mutable references let you modify.

```rust
fn add_exclamation(s: &mut String) {
    s.push_str("!");
}

fn main() {
    let mut s = String::from("hello"); // must be declared `mut`
    add_exclamation(&mut s);           // create a mutable reference
    println!("{s}");                   // prints: hello!
}
```

Two requirements:
1. The variable itself must be declared `mut`.
2. The reference must be created with `&mut`.

If either is missing, the compiler rejects it.

### The Borrowing Rules

Rust enforces two rules at compile time. These rules are the reason Rust can guarantee freedom from data races:

**Rule 1: You can have EITHER multiple immutable references OR exactly one mutable reference, but NOT both at the same time.**

```rust
fn main() {
    let mut s = String::from("hello");

    let r1 = &s;     // OK: first immutable reference
    let r2 = &s;     // OK: second immutable reference
    println!("{r1}, {r2}");
    // r1 and r2 are no longer used after this point

    let r3 = &mut s; // OK: mutable reference (no immutable refs are active)
    r3.push_str(" world");
    println!("{r3}");
}
```

**Rule 2: References must always be valid** (no dangling references).

Why these rules? Think about concurrent access:
- Multiple readers (immutable references): safe, because nobody is modifying the data.
- One writer (mutable reference): safe, because nobody else is reading or writing.
- Reader + writer simultaneously: **unsafe**, because the reader might see partially modified data.

Rust prevents the third case at compile time. This is the same problem that mutexes solve at runtime in other languages, except Rust catches it before the program ever runs.

### Non-Lexical Lifetimes (NLL)

In early Rust, a reference was valid for the entire scope of the variable. Modern Rust (since 2018 edition) uses Non-Lexical Lifetimes: a reference's scope ends at its **last use**, not at the end of the enclosing block.

```rust
fn main() {
    let mut s = String::from("hello");

    let r1 = &s;
    let r2 = &s;
    println!("{r1} and {r2}");
    // -- r1 and r2 scope ends here (last use)

    // This is OK because r1 and r2 are no longer in scope
    let r3 = &mut s;
    r3.push_str(" world");
    println!("{r3}");
}
```

Without NLL, this would not compile because `r1` and `r2` would be alive until the end of `main`, overlapping with `r3`. NLL makes the compiler smarter about when references actually end.

### Dangling Reference Prevention

In C, you can return a pointer to a local variable. The variable is freed when the function returns, and the pointer becomes "dangling" -- pointing to memory that no longer belongs to you. Using it is undefined behavior.

Rust prevents this at compile time:

```rust
// This does NOT compile
fn dangle() -> &String {
    let s = String::from("hello");
    &s // ERROR: s is dropped at the end of this function
}
```

The compiler knows that `s` will be freed when `dangle` returns, so it refuses to let you create a reference to it. The fix: return the `String` directly (transfer ownership).

```rust
fn no_dangle() -> String {
    let s = String::from("hello");
    s // ownership is moved to the caller -- no dangling reference
}
```

## Exercises

### Exercise 1: Immutable Borrowing

This exercise shows how references let you use values without taking ownership.

Create a new project:

```
$ cargo new borrowing
$ cd borrowing
```

Create `src/main.rs`:

```rust
fn first_char(s: &String) -> Option<char> {
    s.chars().next()
}

fn count_vowels(s: &String) -> usize {
    s.chars()
        .filter(|c| matches!(c, 'a' | 'e' | 'i' | 'o' | 'u' | 'A' | 'E' | 'I' | 'O' | 'U'))
        .count()
}

fn main() {
    let message = String::from("Hello Rust");

    // Both functions borrow message -- neither takes ownership
    let first = first_char(&message);
    let vowels = count_vowels(&message);

    // message is still valid
    println!("Message: {message}");
    println!("First char: {:?}", first);
    println!("Vowel count: {vowels}");

    // We can borrow it again, as many times as we want
    let vowels2 = count_vowels(&message);
    println!("Still works: {vowels2}");
}
```

**What's happening here:**
1. `first_char` and `count_vowels` both take `&String` -- they borrow without owning.
2. `message` stays valid through all the calls because no ownership was transferred.
3. Multiple immutable borrows can coexist -- there is no conflict because none of them modify the data.

```
$ cargo run
Message: Hello Rust
First char: Some('H')
Vowel count: 3
Still works: 3
```

### Exercise 2: Mutable Borrowing

This exercise demonstrates modifying borrowed values.

Replace `src/main.rs`:

```rust
fn append_greeting(buffer: &mut String, name: &str) {
    buffer.push_str("Hello, ");
    buffer.push_str(name);
    buffer.push('!');
    buffer.push('\n');
}

fn make_uppercase(s: &mut String) {
    *s = s.to_uppercase();
}

fn main() {
    let mut output = String::new();

    append_greeting(&mut output, "Alice");
    append_greeting(&mut output, "Bob");
    append_greeting(&mut output, "Charlie");

    println!("Greetings:\n{output}");

    let mut shout = String::from("whisper");
    println!("Before: {shout}");
    make_uppercase(&mut shout);
    println!("After: {shout}");
}
```

**What's happening here:**
1. `append_greeting` takes `&mut String` so it can modify the buffer in place.
2. Each call to `append_greeting` borrows `output` mutably, modifies it, and the borrow ends when the function returns.
3. `make_uppercase` replaces the entire string content. `*s = ...` dereferences the mutable reference to assign a new value.
4. Notice that `name: &str` in `append_greeting` is an immutable borrow -- we only need to read the name.

```
$ cargo run
Greetings:
Hello, Alice!
Hello, Bob!
Hello, Charlie!

Before: whisper
After: WHISPER
```

### Exercise 3: The Borrowing Rules in Action

This exercise deliberately violates the borrowing rules so you can see the compiler errors.

Replace `src/main.rs`:

```rust
fn main() {
    let mut data = String::from("hello");

    // --- Scenario 1: Multiple immutable refs -- OK ---
    let r1 = &data;
    let r2 = &data;
    println!("Scenario 1: {r1} and {r2}");

    // --- Scenario 2: Mutable ref after immutable refs are done -- OK ---
    // r1 and r2 are no longer used (NLL drops them here)
    let r3 = &mut data;
    r3.push_str(" world");
    println!("Scenario 2: {r3}");

    // --- Scenario 3: Immutable ref after mutable ref is done -- OK ---
    // r3 is no longer used
    let r4 = &data;
    println!("Scenario 3: {r4}");

    // --- Scenario 4: This would NOT compile ---
    // Uncomment to see the error:
    // let r5 = &data;
    // let r6 = &mut data;
    // println!("{r5}"); // ERROR: r5 (immutable) overlaps with r6 (mutable)
}
```

**What's happening here:**
1. Scenario 1: Two immutable references coexist. No problem.
2. Scenario 2: The immutable references from scenario 1 were last used in the `println!`. NLL recognizes this and considers them expired. So `r3` (mutable) is allowed.
3. Scenario 3: `r3` was last used in the second `println!`. After that, `r4` (immutable) is fine.
4. Scenario 4: If uncommented, `r5` (immutable) and `r6` (mutable) would overlap. The compiler rejects this.

Now uncomment scenario 4 and compile:

```
$ cargo build
error[E0502]: cannot borrow `data` as mutable because it is also borrowed as immutable
  --> src/main.rs:20:14
   |
19 |     let r5 = &data;
   |              ----- immutable borrow occurs here
20 |     let r6 = &mut data;
   |              ^^^^^^^^^ mutable borrow occurs here
21 |     println!("{r5}");
   |               -- immutable borrow later used here
```

The error message explains the conflict precisely. Comment it back out before continuing.

```
$ cargo run
Scenario 1: hello and hello
Scenario 2: hello world
Scenario 3: hello world
```

### Exercise 4: References in Collections

This exercise shows how borrowing works when iterating over collections.

Replace `src/main.rs`:

```rust
fn longest(a: &str, b: &str) -> &str {
    if a.len() >= b.len() {
        a
    } else {
        b
    }
}

fn total_length(words: &[String]) -> usize {
    let mut total = 0;
    for word in words {
        total += word.len();
    }
    total
}

fn main() {
    let words = vec![
        String::from("rust"),
        String::from("ownership"),
        String::from("borrowing"),
    ];

    // Borrow the entire Vec as a slice
    let total = total_length(&words);
    println!("Total characters: {total}");

    // words is still valid -- we only borrowed it
    println!("Words: {:?}", words);

    // Find the longest of two string slices
    let result = longest("hello", "greetings");
    println!("Longest: {result}");
}
```

**What's happening here:**
1. `total_length` takes `&[String]` -- a borrowed slice of Strings. It reads each word without owning the vector.
2. After the call, `words` is still usable because only a reference was passed.
3. `longest` takes two `&str` references and returns a `&str` that borrows from one of the inputs.
4. Note: this `longest` function actually requires lifetime annotations in the general case. It compiles here because of lifetime elision rules -- a topic you will cover later.

```
$ cargo run
Total characters: 22
Words: ["rust", "ownership", "borrowing"]
Longest: greetings
```

### Exercise 5: Combining Ownership and Borrowing

This exercise builds a small program that uses both owned values and references together.

Replace `src/main.rs`:

```rust
fn push_if_long(collection: &mut Vec<String>, candidate: &str, min_length: usize) {
    if candidate.len() >= min_length {
        collection.push(String::from(candidate)); // creates an owned String from the borrowed &str
    }
}

fn print_all(items: &[String]) {
    for (i, item) in items.iter().enumerate() {
        println!("  [{i}] {item}");
    }
}

fn main() {
    let mut long_words: Vec<String> = Vec::new();

    let candidates = ["hi", "ownership", "ok", "borrowing", "no", "reference"];

    for word in candidates {
        push_if_long(&mut long_words, word, 5);
    }

    println!("Long words ({}):", long_words.len());
    print_all(&long_words);

    // We can still use long_words because print_all only borrowed it
    long_words.push(String::from("manually_added"));
    println!("\nAfter adding one more ({}):", long_words.len());
    print_all(&long_words);
}
```

**What's happening here:**
1. `push_if_long` takes a mutable reference to the vector (so it can add items) and an immutable reference to the candidate string (it only needs to read it).
2. Inside the function, `String::from(candidate)` creates an owned `String` from the borrowed `&str` -- the vector needs to own its elements.
3. `print_all` takes an immutable reference to the vector slice -- it only reads.
4. After `print_all` returns, we can still mutate `long_words` because the borrow has ended.

```
$ cargo run
Long words (3):
  [0] ownership
  [1] borrowing
  [2] reference

After adding one more (4):
  [0] ownership
  [1] borrowing
  [2] reference
  [3] manually_added
```

## Common Mistakes

### Modifying Through an Immutable Reference

```rust
fn main() {
    let s = String::from("hello");
    let r = &s;
    r.push_str(" world"); // ERROR
}
```

```
error[E0596]: cannot borrow `*r` as mutable, as it is behind a `&` reference
```

`&s` creates an immutable reference. You cannot modify through it. Use `&mut s` (and declare `s` as `mut`):

```rust
fn main() {
    let mut s = String::from("hello");
    let r = &mut s;
    r.push_str(" world"); // OK
    println!("{r}");
}
```

### Borrowing While Mutably Borrowed

```rust
fn main() {
    let mut v = vec![1, 2, 3];
    let first = &v[0];   // immutable borrow of v
    v.push(4);            // mutable borrow of v -- ERROR
    println!("{first}");
}
```

```
error[E0502]: cannot borrow `v` as mutable because it is also borrowed as immutable
```

This one is subtle. Why can't you push to a vector while holding a reference to one of its elements? Because `push` might reallocate the vector's internal buffer to a new heap location, making `first` a dangling pointer. Rust's borrow checker prevents this at compile time.

Fix: use `first` before modifying `v`:

```rust
fn main() {
    let mut v = vec![1, 2, 3];
    let first = &v[0];
    println!("{first}");  // use it here
    // first's borrow ends (NLL)
    v.push(4);            // now this is OK
    println!("{:?}", v);
}
```

### Returning a Reference to a Local Variable

```rust
fn make_string() -> &String {
    let s = String::from("hello");
    &s
}
```

```
error[E0106]: missing lifetime specifier
```

Even with a lifetime annotation, this would not compile because `s` is dropped when the function returns, making the reference invalid. Return the owned `String` instead:

```rust
fn make_string() -> String {
    String::from("hello")
}
```

## Verification

```
$ cargo build
   Compiling borrowing v0.1.0
    Finished `dev` profile

$ cargo run
Long words (3):
  [0] ownership
  [1] borrowing
  [2] reference

After adding one more (4):
  [0] ownership
  [1] borrowing
  [2] reference
  [3] manually_added

$ cargo clippy
    Finished `dev` profile
```

## Summary

- **Key concepts**: immutable references (`&T`), mutable references (`&mut T`), the borrowing rules (multiple readers XOR one writer), dangling reference prevention, Non-Lexical Lifetimes
- **What you practiced**: passing references to functions instead of transferring ownership, modifying values through mutable references, recognizing and fixing borrowing rule violations, understanding NLL reference scopes
- **Important to remember**: references do not own data -- they borrow it. The compiler enforces at compile time that no reference outlives the data it points to, and that mutable access is exclusive. These two guarantees eliminate data races and use-after-free bugs without any runtime cost.

## What's Next

You have been working with `&String` and full `String` values. But there is a more flexible type for working with string data: **slices**. The next exercise covers `&str` (string slices) and `&[T]` (array slices), which let you reference a portion of data without owning or copying it.

## Resources

- [The Rust Book -- References and Borrowing](https://doc.rust-lang.org/book/ch04-02-references-and-borrowing.html)
- [Rust Reference -- Reference types](https://doc.rust-lang.org/reference/types/pointer.html#shared-references-)
- [Rust Blog -- Non-Lexical Lifetimes](https://blog.rust-lang.org/2018/12/06/Rust-1.31-and-rust-2018.html#non-lexical-lifetimes)
