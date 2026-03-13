# 4. Closures

**Difficulty**: Intermedio

## Prerequisites
- Completed: 01-basico exercises (ownership, borrowing, functions)
- Completed: 02-intermedio/01-traits (trait bounds, Fn traits)
- Completed: 02-intermedio/03-lifetimes (how references are tracked)

## Learning Objectives
After completing this exercise, you will be able to:
- Write closures with various capture modes (by reference, mutable reference, value)
- Distinguish between Fn, FnMut, and FnOnce and when each applies
- Use `move` to force ownership transfer into closures
- Pass closures as function parameters and return values
- Store closures in structs for deferred execution

## Concepts

### What's a Closure?

A closure is an anonymous function that can capture variables from its surrounding scope. You've already used them with iterators — `vec.iter().map(|x| x * 2)`. The `|x| x * 2` part is a closure.

Unlike regular functions, closures don't need type annotations in most cases — the compiler infers them from usage:

```rust
let add_one = |x| x + 1;      // inferred: i32 -> i32
let add = |x, y| x + y;       // inferred from first call
println!("{}", add(2, 3));     // 5
```

### How Closures Capture Variables

Closures capture variables from their environment in the least restrictive way possible:

```rust
let name = String::from("Alice");

// Captures `name` by immutable reference (&name)
let greet = || println!("Hello, {}", name);
greet();
println!("{}", name); // name is still usable — it was only borrowed

// Captures `count` by mutable reference (&mut count)
let mut count = 0;
let mut increment = || { count += 1; };
increment();
increment();
println!("{}", count); // 2

// Captures `data` by value (moves it)
let data = vec![1, 2, 3];
let consume = || {
    let _owned = data; // data is moved into the closure's body
};
consume();
// println!("{:?}", data); // ERROR — data was moved
```

The compiler picks the capture mode automatically based on what the closure body does with each variable.

### The Three Fn Traits

Every closure implements one or more of these traits:

| Trait | Captures | Can call | Analogy |
|-------|----------|----------|---------|
| `Fn` | `&self` (shared ref) | Many times | Read-only observer |
| `FnMut` | `&mut self` (mutable ref) | Many times | Modifier |
| `FnOnce` | `self` (by value) | Exactly once | Consumer |

The hierarchy: `Fn` is a subtrait of `FnMut`, which is a subtrait of `FnOnce`. So anything that implements `Fn` also implements `FnMut` and `FnOnce`.

This means: if a function takes `impl FnOnce`, you can pass any closure. If it takes `impl Fn`, you can only pass closures that don't mutate or consume their captures.

### The `move` Keyword

`move` forces the closure to take ownership of all captured variables, even if the closure body would only need a reference:

```rust
let name = String::from("Alice");
let greet = move || println!("Hello, {}", name);
// name is now owned by the closure — can't use it here
greet();
greet(); // can still call it multiple times — move affects capture, not the Fn trait
```

`move` is essential when:
- Returning a closure from a function (the captured variables would be dangling otherwise)
- Sending a closure to another thread (`std::thread::spawn` requires `'static`)

## Exercises

### Exercise 1: Capture Modes

Predict the output and then verify:

```rust
fn main() {
    // Closure 1: capture by reference
    let message = String::from("hello");
    let print_msg = || println!("{}", message);
    print_msg();
    print_msg();
    println!("Still have message: {}", message);

    // Closure 2: capture by mutable reference
    let mut total = 0;
    let mut add_to_total = |amount: i32| {
        total += amount;
    };
    add_to_total(5);
    add_to_total(10);
    // NOTE: can't use `total` here while `add_to_total` borrows it mutably
    // The borrow ends when add_to_total is no longer used
    drop(add_to_total); // explicitly end the borrow
    println!("Total: {}", total);

    // Closure 3: capture by value
    let data = vec![1, 2, 3];
    let consume = move || {
        println!("Data: {:?}", data);
        // data is owned by this closure
    };
    consume();
    // TODO: Try calling consume() again. Does it work? Why?
    // TODO: Try using `data` after consume(). What happens?
}
```

After running the code, answer:
1. Is `consume` an `Fn`, `FnMut`, or `FnOnce` closure?
2. If `consume` only reads `data` (via `println!`), why can it be called multiple times even with `move`?

### Exercise 2: Closures as Parameters

```rust
fn apply_twice<F: Fn(i32) -> i32>(f: F, x: i32) -> i32 {
    f(f(x))
}

// TODO: Write a function `apply_and_collect` that takes:
//   - a Vec<i32>
//   - a closure that transforms i32 -> i32
// and returns a new Vec<i32> with the closure applied to each element.
// Use `impl Fn(i32) -> i32` syntax for the closure parameter.

// TODO: Write a function `count_matching` that takes:
//   - a slice &[i32]
//   - a closure that takes &i32 and returns bool
// and returns the count of elements for which the closure returns true.

fn main() {
    // apply_twice
    let double = |x| x * 2;
    println!("{}", apply_twice(double, 3));  // 12

    let add_ten = |x| x + 10;
    println!("{}", apply_twice(add_ten, 5)); // 25

    // apply_and_collect
    let numbers = vec![1, 2, 3, 4, 5];
    let doubled = apply_and_collect(numbers.clone(), |x| x * 2);
    println!("{:?}", doubled); // [2, 4, 6, 8, 10]

    let squared = apply_and_collect(numbers.clone(), |x| x * x);
    println!("{:?}", squared); // [1, 4, 9, 16, 25]

    // count_matching
    let count = count_matching(&numbers, |x| x % 2 == 0);
    println!("Even numbers: {}", count); // 2

    let count = count_matching(&numbers, |x| *x > 3);
    println!("Greater than 3: {}", count); // 2
}
```

### Try It Yourself

Write a function `compose` that takes two closures `f: impl Fn(i32) -> i32` and `g: impl Fn(i32) -> i32` and returns a closure that applies `f` first, then `g`. The return type should be `impl Fn(i32) -> i32`. You'll need `move` on the returned closure.

### Exercise 3: FnMut in Practice

```rust
fn for_each_mut<T, F>(items: &[T], mut f: F)
where
    F: FnMut(&T),
{
    for item in items {
        f(item);
    }
}

fn main() {
    let words = vec!["hello", "world", "rust"];

    // TODO: Use for_each_mut with a closure that builds a single String
    // containing all words separated by spaces.
    // Hint: You need a mutable String outside the closure.
    // The closure captures it by &mut reference.

    let mut result = String::new();

    // TODO: Write the for_each_mut call here.
    // After the call, result should be "hello world rust "
    // (trailing space is fine)

    println!("Combined: '{}'", result.trim());

    // TODO: Use for_each_mut with a closure that counts how many words
    // have length > 4.
    let mut long_count = 0;
    // TODO: Write the for_each_mut call here.

    println!("Long words: {}", long_count);
}
```

Why does `for_each_mut` take `F: FnMut` and not `F: Fn`? What would break if you changed it?

### Exercise 4: Returning Closures

```rust
// TODO: Write a function `make_adder` that takes an i32 and returns
// a closure that adds that number to its argument.
// Signature: fn make_adder(n: i32) -> impl Fn(i32) -> i32
// You'll need `move` because `n` would go out of scope otherwise.

// TODO: Write a function `make_counter` that returns a closure.
// Each time the closure is called, it returns the next number
// starting from 0. (0, 1, 2, 3, ...)
// Signature: fn make_counter() -> impl FnMut() -> i32
// Hint: the closure needs to own a mutable counter variable.

fn main() {
    let add_five = make_adder(5);
    let add_ten = make_adder(10);

    println!("{}", add_five(3));  // 8
    println!("{}", add_ten(3));   // 13
    println!("{}", add_five(0));  // 5

    let mut counter = make_counter();
    println!("{}", counter());    // 0
    println!("{}", counter());    // 1
    println!("{}", counter());    // 2
}
```

### Exercise 5: Storing Closures in Structs

```rust
struct Cacher<F>
where
    F: Fn(i32) -> i32,
{
    calculation: F,
    cache: std::collections::HashMap<i32, i32>,
}

// TODO: Implement these methods for Cacher<F>:
//
// fn new(calculation: F) -> Self
//   Initialize with the closure and an empty HashMap.
//
// fn get(&mut self, arg: i32) -> i32
//   If the result for `arg` is cached, return it.
//   Otherwise, call self.calculation with arg, store the result, and return it.
//   Hint: use the entry API or a simple if/else with contains_key.

fn main() {
    let mut expensive = Cacher::new(|x| {
        println!("  Computing for {}...", x);
        x * x + 1
    });

    println!("Result: {}", expensive.get(4));  // prints "Computing..." then 17
    println!("Result: {}", expensive.get(4));  // no "Computing..." — cached! Still 17
    println!("Result: {}", expensive.get(7));  // prints "Computing..." then 50
    println!("Result: {}", expensive.get(4));  // cached: 17
    println!("Result: {}", expensive.get(7));  // cached: 50
}
```

Expected output:
```
  Computing for 4...
Result: 17
Result: 17
  Computing for 7...
Result: 50
Result: 17
Result: 50
```

## Common Mistakes

### Mistake 1: Mutably Borrowing While Closure Holds a Mutable Borrow

```rust
let mut data = vec![1, 2, 3];
let mut push_four = || data.push(4);

println!("{:?}", data); // ERROR — data is mutably borrowed by push_four
push_four();
```

**Error**: `cannot borrow 'data' as immutable because it is also borrowed as mutable`

**Fix**: Use the closure first, then access the data after the closure's borrow ends:

```rust
let mut data = vec![1, 2, 3];
let mut push_four = || data.push(4);
push_four();
drop(push_four); // borrow ends
println!("{:?}", data); // [1, 2, 3, 4]
```

### Mistake 2: Forgetting `move` When Returning a Closure

```rust
fn make_greeter(name: String) -> impl Fn() {
    || println!("Hello, {}", name) // ERROR — name is borrowed, but it will be dropped
}
```

**Error**: `closure may outlive the current function, but it borrows 'name'`

**Fix**:

```rust
fn make_greeter(name: String) -> impl Fn() {
    move || println!("Hello, {}", name)
}
```

### Mistake 3: Using FnOnce When You Need Multiple Calls

```rust
fn call_twice<F: FnOnce()>(f: F) {
    f();
    f(); // ERROR — f was consumed on the first call
}
```

**Error**: `use of moved value: 'f'`

**Fix**: Use `Fn` or `FnMut` instead of `FnOnce`.

## Verification

```bash
cargo run
```

For each exercise:
1. Verify the output matches expectations.
2. Try changing `Fn` to `FnOnce` in function signatures — does the compiler accept it? Where does it break?
3. In Exercise 5, try calling `expensive.get` with the same argument twice and confirm the closure body runs only once.

## Summary

Closures capture variables from their environment by reference, mutable reference, or value — the compiler picks the least restrictive mode. The `Fn`, `FnMut`, and `FnOnce` traits correspond to these capture modes. Use `move` to force ownership transfer, especially when returning closures or sending them across threads. Closures can be stored in structs, enabling patterns like caching and callbacks.

## What's Next

Exercise 05-iterators uses closures extensively — `map`, `filter`, `fold`, and friends all take closures. You'll combine everything from this exercise with iterator chains.

## Resources

- [The Rust Book — Closures](https://doc.rust-lang.org/book/ch13-01-closures.html)
- [Rust by Example — Closures](https://doc.rust-lang.org/rust-by-example/fn/closures.html)
- [Fn vs FnMut vs FnOnce](https://doc.rust-lang.org/std/ops/trait.Fn.html)
