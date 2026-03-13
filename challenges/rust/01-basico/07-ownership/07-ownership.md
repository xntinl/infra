# 7. Ownership

**Difficulty**: Basico

## Prerequisites
- Exercises 01-06 completed (variables, types, functions, control flow)
- Understanding of `let` bindings, mutability, and basic types
- Familiarity with `String` vs `&str` (you have seen both; now you will understand why both exist)

## Learning Objectives
After completing this exercise, you will be able to:
- Explain the difference between stack and heap memory allocation
- State the three ownership rules and apply them to code
- Predict when a value will be moved vs copied
- Identify compiler errors caused by use-after-move
- Use `Clone` for explicit deep copies when you need them
- Trace ownership transfer through function calls and return values

## Concepts

### Why Ownership Exists

Every program must manage memory. There are three common approaches:

1. **Garbage collector** (Java, Go, Python): a runtime process periodically scans memory and frees unused objects. Simple for the programmer, but adds latency and uses extra memory.
2. **Manual management** (C, C++): the programmer calls `malloc`/`free` or `new`/`delete`. Maximum control, but use-after-free bugs, double frees, and memory leaks are common and catastrophic.
3. **Ownership** (Rust): the compiler tracks who "owns" each piece of memory and inserts deallocation code at compile time. No runtime cost, no garbage collector, no manual free calls.

Ownership is Rust's answer to memory safety. It is enforced entirely at compile time -- there is zero runtime overhead.

### Stack vs Heap

Before understanding ownership, you need to understand where data lives.

**The stack** is a fixed-size, LIFO (last in, first out) region of memory. Pushing and popping is extremely fast. Every function call creates a stack frame that holds local variables. When the function returns, its stack frame is popped and the memory is immediately available for reuse.

Limitation: the compiler must know the exact size of every value on the stack at compile time.

**The heap** is a large pool of memory where you can allocate blocks of arbitrary size at runtime. Allocation is slower because the allocator must find a free block. Access is slower because it requires following a pointer.

```
Stack                          Heap
+------------------+           +----------------------------+
| main()           |           |                            |
|   s1: ptr -------+---------->| "hello" (5 bytes of UTF-8) |
|       len: 5     |           |                            |
|       cap: 5     |           +----------------------------+
|                  |
|   x: 42 (i32)   |  <-- lives entirely on the stack
+------------------+
```

An `i32` like `42` is 4 bytes, known at compile time -- it goes on the stack. A `String` like `"hello"` can grow or shrink at runtime, so its character data lives on the heap. The stack holds a small struct (pointer, length, capacity) that points to the heap data.

### The Three Ownership Rules

Memorize these. Everything else follows from them.

1. **Each value in Rust has exactly one owner** (a variable).
2. **There can only be one owner at a time.**
3. **When the owner goes out of scope, the value is dropped** (its memory is freed).

```rust
fn main() {
    {
        let s = String::from("hello"); // s owns the String
        println!("{s}");
    } // s goes out of scope here. Rust calls `drop(s)`, freeing the heap memory.

    // s no longer exists here -- the memory is gone.
}
```

This is similar to C++ RAII (Resource Acquisition Is Initialization), where destructors run when objects leave scope. The difference is that Rust's ownership rules make this safe by preventing you from using freed memory.

### Move Semantics

This is where Rust diverges from every language you have used before.

```rust
fn main() {
    let s1 = String::from("hello");
    let s2 = s1; // s1 is MOVED to s2

    // println!("{s1}"); // ERROR: s1 is no longer valid
    println!("{s2}"); // OK: s2 is the owner now
}
```

What happens at `let s2 = s1`?

```
BEFORE the move:                AFTER the move:

Stack                           Stack
+------------------+            +------------------+
| s1: ptr ----+--> heap         | s1: (INVALID)    |
|     len: 5  |                 |                  |
|     cap: 5  |                 | s2: ptr ----+--> heap
+-------------+----+            |     len: 5  |
                                |     cap: 5  |
                                +-------------+----+

                                Heap (same allocation!)
                                +----------------------------+
                                | "hello" (5 bytes of UTF-8) |
                                +----------------------------+
```

Rust does NOT copy the heap data. It copies the stack struct (pointer, length, capacity) from `s1` to `s2`, and then **invalidates** `s1`. This is a "move" -- ownership transfers from `s1` to `s2`.

Why not just copy the pointer and keep both valid (like C would)? Because when both go out of scope, Rust would call `drop` on both, freeing the same heap memory twice. A double free is undefined behavior. Moves prevent this.

Compare with other languages:
- **C**: You could copy the pointer and have two aliases. Double free is your problem.
- **C++**: Copy constructor would allocate new heap memory and copy the bytes. Safe but expensive.
- **Python**: Both variables point to the same object, reference counted. When the count reaches zero, the GC frees it.
- **Rust**: One owner at a time. No double free, no reference counting, no deep copy unless you ask for it.

### Copy: Stack-Only Types

Moves would be annoying for simple types like integers. If `let y = x` invalidated `x` for an `i32`, you would never get anything done.

Types that implement the `Copy` trait are **copied** instead of moved. The original remains valid.

```rust
fn main() {
    let x: i32 = 42;
    let y = x; // x is COPIED, not moved

    println!("x = {x}"); // OK! x is still valid
    println!("y = {y}"); // y is an independent copy
}
```

Which types are `Copy`?
- All integer types (`i32`, `u64`, etc.)
- `bool`
- `char`
- `f32`, `f64`
- Tuples, **if** all their elements are `Copy`: `(i32, bool)` is `Copy`, but `(i32, String)` is not.

The rule: if a type's data lives entirely on the stack and does not need cleanup when dropped, it can be `Copy`. `String` is not `Copy` because it owns heap memory that must be freed.

### Clone: Explicit Deep Copy

When you need a real copy of a heap-allocated value, call `.clone()`. This allocates new heap memory and copies the data.

```rust
fn main() {
    let s1 = String::from("hello");
    let s2 = s1.clone(); // deep copy: new heap allocation

    println!("s1 = {s1}"); // OK: s1 is still valid
    println!("s2 = {s2}"); // OK: s2 is an independent copy
}
```

```
After clone:

Stack                           Heap
+------------------+            +----------------------------+
| s1: ptr ----------+---------->| "hello" (5 bytes)          |
|     len: 5       |            +----------------------------+
|     cap: 5       |
|                  |            +----------------------------+
| s2: ptr ----------+---------->| "hello" (5 bytes)          |
|     len: 5       |            +----------------------------+
|     cap: 5       |
+------------------+
```

`.clone()` is explicit. You see it in the code, you know a potentially expensive allocation is happening. Rust never does this silently.

### Ownership and Functions

Passing a value to a function follows the same rules as assignment. If the type is `Copy`, the function gets a copy. Otherwise, the value is **moved** into the function, and the caller can no longer use it.

```rust
fn takes_ownership(s: String) {
    println!("Got: {s}");
} // s is dropped here -- the String is freed

fn makes_copy(n: i32) {
    println!("Got: {n}");
} // n is dropped, but i32 is Copy -- nothing special happens

fn main() {
    let greeting = String::from("hello");
    takes_ownership(greeting);
    // println!("{greeting}"); // ERROR: greeting was moved

    let number = 42;
    makes_copy(number);
    println!("{number}"); // OK: i32 is Copy
}
```

This is the same principle: `String` is moved, `i32` is copied. The function parameter becomes the new owner.

### Return Values Transfer Ownership

Functions can give ownership back to the caller through return values.

```rust
fn create_greeting(name: &str) -> String {
    let greeting = String::from("Hello, ");
    greeting + name // ownership of the result transfers to the caller
}

fn main() {
    let msg = create_greeting("Rust");
    println!("{msg}"); // msg owns the String
}
```

This is how you get around the "moved into function" problem: the function creates or transforms a value and returns ownership to the caller.

## Exercises

### Exercise 1: Observing Move Semantics

This exercise makes the move visible by showing what compiles and what does not.

Create a new project:

```
$ cargo new ownership
$ cd ownership
```

Create `src/main.rs`:

```rust
fn main() {
    // Integers: Copy
    let a = 10;
    let b = a;
    println!("a = {a}, b = {b}"); // both valid

    // Strings: Move
    let s1 = String::from("ownership");
    let s2 = s1;
    // At this point, s1 is invalid. s2 owns the String.
    println!("s2 = {s2}");

    // Uncomment the next line to see the compiler error:
    // println!("s1 = {s1}");

    // Booleans: Copy
    let flag1 = true;
    let flag2 = flag1;
    println!("flag1 = {flag1}, flag2 = {flag2}");

    // Tuples of Copy types: Copy
    let point1 = (3, 4);
    let point2 = point1;
    println!("point1 = {:?}, point2 = {:?}", point1, point2);

    // Tuple with a String: Move
    let labeled = (1, String::from("first"));
    let labeled2 = labeled;
    // println!("{:?}", labeled); // ERROR: moved
    println!("{:?}", labeled2);
}
```

**What's happening here:**
1. `a` to `b`: `i32` is `Copy`, so `a` remains valid.
2. `s1` to `s2`: `String` is not `Copy`, so `s1` is moved and becomes invalid.
3. `flag1` to `flag2`: `bool` is `Copy`.
4. `point1` to `point2`: a tuple of two `i32` values is `Copy` because both elements are `Copy`.
5. `labeled` to `labeled2`: the tuple contains a `String`, so the entire tuple is moved.

```
$ cargo run
a = 10, b = 10
s2 = ownership
flag1 = true, flag2 = true
point1 = (3, 4), point2 = (3, 4)
(1, "first")
```

Now uncomment the `println!("s1 = {s1}");` line and try to compile:

```
$ cargo build
error[E0382]: borrow of moved value: `s1`
 --> src/main.rs:10:24
  |
7 |     let s1 = String::from("ownership");
  |         -- move occurs because `s1` has type `String`, which does not implement the `Copy` trait
8 |     let s2 = s1;
  |              -- value moved here
9 |
10|     println!("s1 = {s1}");
  |                     ^^ value borrowed here after move
```

Read that error carefully. The compiler tells you exactly what happened: `s1` was moved on line 8, and you tried to use it on line 10. Comment the line back out before continuing.

### Exercise 2: Clone for Deep Copies

This exercise shows when and how to use `.clone()`.

Replace `src/main.rs`:

```rust
fn main() {
    let original = String::from("deep copy me");
    let cloned = original.clone();

    println!("original: {original}");
    println!("cloned:   {cloned}");

    // They are independent -- modifying one does not affect the other
    let mut s1 = String::from("hello");
    let s2 = s1.clone();

    s1.push_str(" world");

    println!("s1: {s1}");
    println!("s2: {s2}"); // s2 is still "hello"
}
```

**What's happening here:**
1. `.clone()` creates a completely independent copy on the heap.
2. After cloning, `original` and `cloned` are separate allocations.
3. Mutating `s1` after cloning does not affect `s2`.

```
$ cargo run
original: deep copy me
cloned:   deep copy me
s1: hello world
s2: hello
```

### Exercise 3: Ownership Through Function Calls

This exercise traces ownership as values move into and out of functions.

Replace `src/main.rs`:

```rust
fn take_and_return(s: String) -> String {
    println!("  inside take_and_return: {s}");
    s // return ownership to the caller
}

fn take_and_drop(s: String) {
    println!("  inside take_and_drop: {s}");
} // s is dropped here

fn give_new() -> String {
    String::from("brand new")
}

fn main() {
    let s1 = String::from("hello");
    println!("1. s1 created: {s1}");

    // Move into function, get it back
    let s2 = take_and_return(s1);
    // s1 is invalid now, s2 owns the String
    println!("2. s2 received back: {s2}");

    // Move into function, do NOT get it back
    take_and_drop(s2);
    // s2 is invalid now, the String has been freed
    // println!("{s2}"); // ERROR: moved

    // Get a new String from a function
    let s3 = give_new();
    println!("3. s3 from give_new: {s3}");

    // Copy type: function gets a copy, original is fine
    let num = 42;
    consume_number(num);
    println!("4. num is still: {num}");
}

fn consume_number(n: i32) {
    println!("  inside consume_number: {n}");
}
```

**What's happening here:**
1. `s1` moves into `take_and_return`. The function uses it and returns it -- ownership transfers back to `s2`.
2. `s2` moves into `take_and_drop`. The function does not return it, so the `String` is freed when the function ends.
3. `give_new` creates a `String` inside the function and returns ownership to the caller (`s3`).
4. `num` is `i32` (Copy), so `consume_number` gets a copy and `num` stays valid.

What do you think this will print?

```
$ cargo run
1. s1 created: hello
  inside take_and_return: hello
2. s2 received back: hello
  inside take_and_drop: hello
3. s3 from give_new: brand new
  inside consume_number: 42
4. num is still: 42
```

### Exercise 4: Ownership in a Loop

This is a common stumbling point. What happens when you use a `String` inside a loop?

Replace `src/main.rs`:

```rust
fn print_greeting(name: String) {
    println!("Hello, {name}!");
}

fn main() {
    let name = String::from("Rustacean");

    // This will NOT compile:
    // for _ in 0..3 {
    //     print_greeting(name); // name is moved on the first iteration!
    // }

    // Fix 1: Clone each iteration
    for _ in 0..3 {
        print_greeting(name.clone());
    }

    // name is still valid because we only cloned it
    println!("Original name: {name}");

    // Fix 2: Use a reference (preview of Exercise 08)
    for _ in 0..3 {
        print_greeting_ref(&name);
    }
}

fn print_greeting_ref(name: &str) {
    println!("Hello, {name}!");
}
```

**What's happening here:**
1. If you passed `name` directly in the loop, the first iteration would move it, and the second iteration would fail to compile.
2. Fix 1: `.clone()` creates a new `String` each iteration. The original `name` stays valid. This works but allocates memory every time.
3. Fix 2: Pass a reference (`&name`) instead of the value. The function borrows the data without taking ownership. This is the idiomatic solution, which you will learn in the next exercise.

```
$ cargo run
Hello, Rustacean!
Hello, Rustacean!
Hello, Rustacean!
Original name: Rustacean
Hello, Rustacean!
Hello, Rustacean!
Hello, Rustacean!
```

### Exercise 5: Ownership Tracing Challenge

This exercise tests your understanding. Read the code and predict the output before running it.

Replace `src/main.rs`:

```rust
fn first_word_length(s: String) -> (String, usize) {
    let len = s.len();
    (s, len) // return ownership AND the length
}

fn main() {
    let words = vec![
        String::from("hello"),
        String::from("ownership"),
        String::from("rust"),
    ];

    // Move each word out of the vector
    for word in words {
        let (returned_word, length) = first_word_length(word);
        println!("{returned_word} has {length} bytes");
    }

    // println!("{:?}", words); // ERROR: words was moved in the for loop

    // Integers in a vector: Copy behavior
    let numbers = vec![10, 20, 30];
    for n in numbers.iter() {
        println!("number: {n}");
    }
    // numbers is still valid because .iter() borrows, it does not move
    println!("numbers: {:?}", numbers);
}
```

**What's happening here:**
1. `for word in words` consumes the vector. Each `word` takes ownership of a `String` from the vector. After the loop, `words` is invalid.
2. `first_word_length` takes a `String` by value (move), then returns it as part of a tuple. The caller gets ownership back.
3. `for n in numbers.iter()` borrows each element. The vector is not consumed. After the loop, `numbers` is still valid.

What do you think this will print?

```
$ cargo run
hello has 5 bytes
ownership has 9 bytes
rust has 4 bytes
number: 10
number: 20
number: 30
numbers: [10, 20, 30]
```

## Common Mistakes

### Using a Value After Moving It

This is the single most common mistake for Rust beginners.

```rust
fn main() {
    let s = String::from("hello");
    let s2 = s;
    println!("{s}");
}
```

```
error[E0382]: borrow of moved value: `s`
```

The fix depends on what you need:
- If you need two independent copies: use `.clone()`
- If you just need to read the value: use a reference (next exercise)
- If you are done with the original: just use `s2`

### Assuming Vectors Are Copy

```rust
fn main() {
    let v1 = vec![1, 2, 3];
    let v2 = v1;
    println!("{:?}", v1); // ERROR: moved
}
```

Even though `Vec<i32>` contains `Copy` types, the `Vec` itself is not `Copy`. It owns heap-allocated memory (the array buffer), so it follows move semantics. Use `v1.clone()` if you need both.

### Forgetting That String::from() Returns an Owned String

```rust
fn main() {
    let s1 = String::from("hello");
    let s2 = String::from("hello");

    // s1 and s2 are independent Strings, each with their own heap allocation.
    // They happen to contain the same bytes, but they are not the same object.
}
```

This is different from Python, where `s1 = "hello"` and `s2 = "hello"` might point to the same interned string object.

### Moving Out of a Collection by Index

```rust
fn main() {
    let names = vec![String::from("alice"), String::from("bob")];
    let first = names[0]; // ERROR: cannot move out of index
}
```

```
error[E0507]: cannot move out of index of `Vec<String>`
```

You cannot move a value out of a `Vec` by index because that would leave a "hole" in the vector. Use `.clone()`, references, or methods like `.remove()` or `.swap_remove()`:

```rust
fn main() {
    let names = vec![String::from("alice"), String::from("bob")];
    let first = names[0].clone(); // clone it
    println!("{first}");
    println!("{:?}", names); // names is still intact
}
```

## Verification

```
$ cargo build
   Compiling ownership v0.1.0
    Finished `dev` profile

$ cargo run
hello has 5 bytes
ownership has 9 bytes
rust has 4 bytes
number: 10
number: 20
number: 30
numbers: [10, 20, 30]

$ cargo clippy
    Finished `dev` profile
```

## Summary

- **Key concepts**: stack vs heap, the three ownership rules (one owner, exclusive, drop on scope exit), move semantics, `Copy` for stack types, `Clone` for explicit deep copies, ownership transfer through function calls and returns
- **What you practiced**: tracing ownership through assignments and function calls, identifying which types are `Copy` vs moved, using `.clone()` when you need independent copies, reading compiler errors about moved values
- **Important to remember**: when a `String` (or `Vec`, `Box`, or any heap-allocating type) is assigned to another variable or passed to a function, the original is invalidated. This prevents double frees at compile time with zero runtime cost. Use `.clone()` when you genuinely need two copies. The next exercise introduces references, which let you use a value without taking ownership.

## What's Next

Transferring ownership everywhere is cumbersome. What if you just want to *look at* a value without taking ownership? That is what **References and Borrowing** solves. You will learn to pass values to functions without moving them, opening up much more flexible patterns.

## Resources

- [The Rust Book -- What is Ownership?](https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html)
- [Rust Reference -- Destructors](https://doc.rust-lang.org/reference/destructors.html)
- [Rust by Example -- Ownership and moves](https://doc.rust-lang.org/rust-by-example/scope/move.html)
- [The Rustonomicon -- Ownership](https://doc.rust-lang.org/nomicon/ownership.html)
