# Enums and Pattern Matching

**Difficulty:** Basico
**Time:** 45-60 minutes
**Prerequisites:** Structs, methods, ownership, references

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** how to define enums with unit, tuple, and struct variants.
- **Understand** why `match` is exhaustive and how that prevents bugs at compile time.
- **Apply** `match`, `if let`, `while let`, and enum methods to build a command parser.

## Concepts

### Why enums exist

A struct says "a value contains all of these fields." An enum says "a value is exactly one of these variants." Together they cover the two fundamental shapes of data: product types (AND) and sum types (OR).

In languages without proper enums you end up with stringly-typed code, boolean flags, or class hierarchies. Rust enums carry data inside each variant and the compiler forces you to handle every case. This eliminates entire categories of "forgot to handle that state" bugs.

### Enum definition

An enum lists its variants. Each variant can carry different kinds of data -- or none at all:

```rust
enum Command {
    Quit,                         // unit variant -- no data
    Echo(String),                 // tuple variant -- one unnamed field
    Move { x: i32, y: i32 },     // struct variant -- named fields
    Color(u8, u8, u8),            // tuple variant -- multiple fields
}
```

A value of type `Command` is always exactly one of those four variants. It cannot be two at once, and it cannot be something else.

### match -- exhaustive pattern matching

The `match` expression is Rust's most powerful control flow tool. It forces you to cover every variant:

```rust
fn process(cmd: Command) {
    match cmd {
        Command::Quit => println!("Quitting"),
        Command::Echo(msg) => println!("{}", msg),
        Command::Move { x, y } => println!("Moving to ({}, {})", x, y),
        Command::Color(r, g, b) => println!("Color: #{:02x}{:02x}{:02x}", r, g, b),
    }
}
```

If you remove one arm the compiler refuses to build. This is exhaustiveness checking and it is why Rust enums are safer than switch statements in other languages.

The catch-all pattern `_` matches anything you did not explicitly handle:

```rust
match cmd {
    Command::Quit => println!("Quitting"),
    _ => println!("Not quitting"),
}
```

### if let and while let

When you only care about one variant, `if let` is cleaner than a full `match`:

```rust
if let Command::Echo(msg) = cmd {
    println!("Echo: {}", msg);
}
```

`while let` loops as long as a pattern matches. It is commonly used with iterators and `Option`:

```rust
let mut stack = vec![1, 2, 3];
while let Some(top) = stack.pop() {
    println!("{}", top);
}
```

### Enum methods via impl

Enums get `impl` blocks just like structs:

```rust
impl Command {
    fn is_quit(&self) -> bool {
        matches!(self, Command::Quit)
    }
}
```

The `matches!` macro is a shorthand that returns `true` if the pattern matches.

## Exercises

### Exercise 1 -- Define and match

What do you think this will print?

```rust
#[derive(Debug)]
enum Direction {
    North,
    South,
    East,
    West,
}

fn describe(dir: &Direction) -> &str {
    match dir {
        Direction::North => "heading north",
        Direction::South => "heading south",
        Direction::East => "heading east",
        Direction::West => "heading west",
    }
}

fn main() {
    let dirs = [Direction::North, Direction::West, Direction::South];
    for d in &dirs {
        println!("{:?}: {}", d, describe(d));
    }
}
```

Predict the output, then run `cargo run`.

### Exercise 2 -- Variants with data

Build an enum where each variant carries different data. Predict the output:

```rust
#[derive(Debug)]
enum Shape {
    Circle(f64),
    Rectangle { width: f64, height: f64 },
    Triangle(f64, f64, f64), // three sides
}

fn area(shape: &Shape) -> f64 {
    match shape {
        Shape::Circle(radius) => std::f64::consts::PI * radius * radius,
        Shape::Rectangle { width, height } => width * height,
        Shape::Triangle(a, b, c) => {
            // Heron's formula
            let s = (a + b + c) / 2.0;
            (s * (s - a) * (s - b) * (s - c)).sqrt()
        }
    }
}

fn main() {
    let shapes = vec![
        Shape::Circle(5.0),
        Shape::Rectangle { width: 4.0, height: 6.0 },
        Shape::Triangle(3.0, 4.0, 5.0),
    ];

    for shape in &shapes {
        println!("{:?} => area = {:.2}", shape, area(shape));
    }
}
```

### Exercise 3 -- Build a command parser

Parse string tokens into a typed `Command` enum. This is a realistic pattern -- raw input goes in, validated types come out:

```rust
#[derive(Debug)]
enum Command {
    Quit,
    Echo(String),
    Move { x: i32, y: i32 },
    Unknown(String),
}

fn parse_command(input: &str) -> Command {
    let parts: Vec<&str> = input.trim().splitn(2, ' ').collect();
    match parts[0] {
        "quit" => Command::Quit,
        "echo" => {
            let msg = if parts.len() > 1 { parts[1] } else { "" };
            Command::Echo(msg.to_string())
        }
        "move" => {
            if parts.len() > 1 {
                let coords: Vec<&str> = parts[1].split(',').collect();
                if coords.len() == 2 {
                    if let (Ok(x), Ok(y)) = (coords[0].trim().parse(), coords[1].trim().parse()) {
                        return Command::Move { x, y };
                    }
                }
            }
            Command::Unknown(input.to_string())
        }
        other => Command::Unknown(other.to_string()),
    }
}

fn execute(cmd: &Command) {
    match cmd {
        Command::Quit => println!("Goodbye!"),
        Command::Echo(msg) => println!("{}", msg),
        Command::Move { x, y } => println!("Moving to ({}, {})", x, y),
        Command::Unknown(raw) => println!("Unknown command: {}", raw),
    }
}

fn main() {
    let inputs = vec![
        "echo hello world",
        "move 10, 20",
        "quit",
        "dance",
        "move bad",
    ];

    for input in inputs {
        let cmd = parse_command(input);
        print!("{:>15} => ", input);
        execute(&cmd);
    }
}
```

Before running, trace through each input and predict what `parse_command` returns.

### Exercise 4 -- if let and while let

Observe how `if let` simplifies code when you only care about one variant:

```rust
#[derive(Debug)]
enum Packet {
    Data(Vec<u8>),
    Heartbeat,
    Error(String),
}

fn main() {
    let packets = vec![
        Packet::Heartbeat,
        Packet::Data(vec![0x48, 0x65, 0x6c, 0x6c, 0x6f]),
        Packet::Error("timeout".to_string()),
        Packet::Data(vec![0x52, 0x75, 0x73, 0x74]),
        Packet::Heartbeat,
    ];

    // Only process Data packets
    for packet in &packets {
        if let Packet::Data(bytes) = packet {
            let text = String::from_utf8_lossy(bytes);
            println!("Received data: {}", text);
        }
    }

    println!("---");

    // while let with a mutable stack
    let mut stack: Vec<Packet> = vec![
        Packet::Data(vec![1, 2, 3]),
        Packet::Heartbeat,
        Packet::Data(vec![4, 5, 6]),
    ];

    while let Some(packet) = stack.pop() {
        println!("Popped: {:?}", packet);
    }
    println!("Stack is empty: {}", stack.is_empty());
}
```

Predict the output, paying attention to the order of `while let` (pop takes from the end).

### Exercise 5 -- Enum methods and nested patterns

Attach methods to an enum and use nested patterns inside `match`:

```rust
#[derive(Debug, Clone)]
enum Severity {
    Info,
    Warning,
    Error,
}

#[derive(Debug)]
enum LogEntry {
    Message { severity: Severity, text: String },
    Metric { name: String, value: f64 },
    Batch(Vec<LogEntry>),
}

impl LogEntry {
    fn is_error(&self) -> bool {
        match self {
            LogEntry::Message { severity: Severity::Error, .. } => true,
            LogEntry::Batch(entries) => entries.iter().any(|e| e.is_error()),
            _ => false,
        }
    }

    fn count(&self) -> usize {
        match self {
            LogEntry::Batch(entries) => entries.iter().map(|e| e.count()).sum(),
            _ => 1,
        }
    }
}

fn main() {
    let log = LogEntry::Batch(vec![
        LogEntry::Message { severity: Severity::Info, text: "started".to_string() },
        LogEntry::Metric { name: "cpu".to_string(), value: 72.5 },
        LogEntry::Message { severity: Severity::Error, text: "disk full".to_string() },
    ]);

    println!("Total entries: {}", log.count());
    println!("Contains error: {}", log.is_error());

    let ok_log = LogEntry::Message {
        severity: Severity::Info,
        text: "all good".to_string(),
    };
    println!("Single entry error: {}", ok_log.is_error());
}
```

## Common Mistakes

**Non-exhaustive match:**

```
error[E0004]: non-exhaustive patterns: `Direction::West` not covered
  --> src/main.rs:10:11
   |
10 |     match dir {
   |           ^^^ pattern `Direction::West` not covered
```

Fix: add the missing arm or use `_` as a catch-all.

**Moving out of an enum when you only have a reference:**

```
error[E0507]: cannot move out of `*packet` which is behind a shared reference
```

Fix: match on `&Packet::Data(ref bytes)` or change the function to take ownership.

**Forgetting the comma after a match arm:**

The compiler usually gives a helpful message, but the error can point to the wrong line. If a `match` gives a confusing error, check that every arm ends with a comma (the last arm's comma is optional but recommended).

## Verification

```bash
# Exercise 1 -- basic enum
cargo run

# Exercise 3 -- command parser (most interesting to trace)
cargo run

# Exercise 4 -- if let / while let
cargo run

# Exercise 5 -- nested patterns
cargo run
```

Confirm exhaustiveness by removing a match arm in Exercise 1 and observing the compiler error.

## Summary

- Enums model "one of these variants" -- the OR to a struct's AND.
- Variants can carry no data (unit), unnamed data (tuple), or named data (struct).
- `match` is exhaustive: the compiler forces you to handle every case.
- `if let` and `while let` are concise alternatives when you care about one variant.
- Enums get `impl` blocks just like structs, enabling methods like `is_error()`.
- Nested patterns (`severity: Severity::Error`) let you destructure deeply in one step.

## What's Next

Option and Result -- Rust's built-in enums for "might be absent" and "might fail." They use everything you learned here and are the foundation of Rust error handling.

## Resources

- [The Rust Book -- Enums](https://doc.rust-lang.org/book/ch06-00-enums.html)
- [The Rust Book -- Pattern Syntax](https://doc.rust-lang.org/book/ch18-03-pattern-syntax.html)
- [Rust By Example -- Enums](https://doc.rust-lang.org/rust-by-example/custom_types/enum.html)
