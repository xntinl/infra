# Structs and Methods

**Difficulty:** Basico
**Time:** 45-60 minutes
**Prerequisites:** Variables, ownership, references, functions

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the syntax for defining structs, tuple structs, and unit structs.
- **Understand** how `impl` blocks attach behavior to data and why Rust separates the two.
- **Apply** methods (`&self`, `&mut self`, `self`) and associated functions to build a domain model.

## Concepts

### Why structs exist

Functions give you behavior. Variables give you data. But real programs need to group related data together and attach behavior to that group. In C you would use a plain struct and pass it to free functions. In Java you would use a class. Rust sits in the middle: you define a struct for the data and an `impl` block for the behavior. The compiler keeps them honest -- if a method only reads, it takes `&self`; if it mutates, it takes `&mut self`; if it consumes, it takes `self`. This is not ceremony for its own sake. It is the ownership system making invalid states unrepresentable at compile time.

### Struct definition and field init shorthand

A struct is a custom type that groups named fields:

```rust
struct Task {
    title: String,
    done: bool,
}
```

When a variable has the same name as a field you can skip the colon:

```rust
fn new_task(title: String) -> Task {
    Task { title, done: false } // field init shorthand
}
```

### Struct update syntax

The `..` operator copies remaining fields from another instance. The source is moved if any moved field is used, otherwise it is still valid:

```rust
let task_a = Task { title: String::from("Buy milk"), done: false };
let task_b = Task { title: String::from("Read book"), ..task_a };
// task_a.done is still accessible (bool is Copy), but task_a.title was moved
```

### Tuple structs and unit structs

A tuple struct has no named fields. Use it when the name of the type is enough context:

```rust
struct Meters(f64);
struct Seconds(f64);
```

A unit struct has no fields at all. It is useful as a marker type or when implementing a trait that carries no data:

```rust
struct Marker;
```

### impl blocks, methods, and associated functions

An `impl` block attaches functions to a type. If the first parameter is some form of `self`, it is a method. Otherwise it is an associated function (often used as a constructor):

```rust
impl Task {
    // Associated function -- no self, called with Task::new(...)
    fn new(title: &str) -> Self {
        Self {
            title: title.to_string(),
            done: false,
        }
    }

    // Method -- borrows immutably
    fn is_done(&self) -> bool {
        self.done
    }

    // Method -- borrows mutably
    fn complete(&mut self) {
        self.done = true;
    }

    // Method -- consumes self
    fn into_title(self) -> String {
        self.title
    }
}
```

`Self` (capital S) is an alias for the type the `impl` block is for. You can have multiple `impl` blocks for the same type -- the compiler merges them.

### Deriving Debug

Adding `#[derive(Debug)]` lets you print a struct with `{:?}`:

```rust
#[derive(Debug)]
struct Task {
    title: String,
    done: bool,
}
```

## Exercises

### Exercise 1 -- Define and construct

What do you think this will print?

```rust
#[derive(Debug)]
struct Task {
    title: String,
    done: bool,
    priority: u8,
}

fn main() {
    let title = String::from("Write tests");
    let task = Task {
        title,       // field init shorthand
        done: false,
        priority: 3,
    };
    println!("{:?}", task);
    println!("Priority: {}", task.priority);
}
```

Write it to `src/main.rs`, predict the output, then run `cargo run`.

### Exercise 2 -- Update syntax and ownership

What do you think this will print? Will it compile?

```rust
#[derive(Debug)]
struct Task {
    title: String,
    done: bool,
    priority: u8,
}

fn main() {
    let task_a = Task {
        title: String::from("Deploy"),
        done: false,
        priority: 1,
    };

    let task_b = Task {
        priority: 5,
        ..task_a
    };

    println!("B: {:?}", task_b);
    // Uncomment the next line. Does it compile? Why or why not?
    // println!("A: {:?}", task_a);
    println!("A priority (Copy type): {}", task_a.priority);
}
```

Predict, then verify. Uncomment the marked line and read the compiler error carefully.

### Exercise 3 -- Methods and associated functions

Build a `TaskList` that holds a `Vec<Task>` and provides methods to add, complete, and query tasks:

```rust
#[derive(Debug)]
struct Task {
    title: String,
    done: bool,
}

#[derive(Debug)]
struct TaskList {
    tasks: Vec<Task>,
}

impl Task {
    fn new(title: &str) -> Self {
        Self {
            title: title.to_string(),
            done: false,
        }
    }
}

impl TaskList {
    fn new() -> Self {
        Self { tasks: Vec::new() }
    }

    fn add(&mut self, title: &str) {
        self.tasks.push(Task::new(title));
    }

    fn complete(&mut self, title: &str) -> bool {
        for task in self.tasks.iter_mut() {
            if task.title == title {
                task.done = true;
                return true;
            }
        }
        false
    }

    fn pending_count(&self) -> usize {
        self.tasks.iter().filter(|t| !t.done).count()
    }

    fn summary(self) -> String {
        let total = self.tasks.len();
        let done = self.tasks.iter().filter(|t| t.done).count();
        format!("{}/{} tasks completed", done, total)
    }
}

fn main() {
    let mut list = TaskList::new();
    list.add("Write code");
    list.add("Review PR");
    list.add("Deploy");

    println!("Pending: {}", list.pending_count());

    list.complete("Review PR");
    println!("Pending after completing one: {}", list.pending_count());

    // summary() takes self by value -- list is consumed
    let s = list.summary();
    println!("{}", s);

    // Uncomment the next line. Why does it fail?
    // println!("Pending: {}", list.pending_count());
}
```

Before running, answer: why does `summary` consume `self`? What happens if you try to use `list` after calling it?

### Exercise 4 -- Tuple structs as newtypes

Newtypes prevent mixing up values that have the same underlying type. Predict the compiler error:

```rust
struct Meters(f64);
struct Seconds(f64);

impl Meters {
    fn value(&self) -> f64 {
        self.0
    }
}

impl Seconds {
    fn value(&self) -> f64 {
        self.0
    }
}

fn speed(distance: Meters, time: Seconds) -> f64 {
    distance.value() / time.value()
}

fn main() {
    let d = Meters(100.0);
    let t = Seconds(9.58);
    println!("Speed: {:.2} m/s", speed(d, t));

    // Uncomment to see the type safety in action:
    // println!("Wrong: {:.2}", speed(t, d));
}
```

### Exercise 5 -- Multiple impl blocks and Debug

Add a second `impl` block that provides a display-oriented method. This demonstrates that multiple `impl` blocks are legal and sometimes useful for organizing code:

```rust
#[derive(Debug)]
struct Task {
    title: String,
    done: bool,
    priority: u8,
}

impl Task {
    fn new(title: &str, priority: u8) -> Self {
        Self {
            title: title.to_string(),
            done: false,
            priority,
        }
    }

    fn complete(&mut self) {
        self.done = true;
    }
}

// Second impl block -- grouping display helpers separately
impl Task {
    fn status_label(&self) -> &str {
        if self.done { "DONE" } else { "TODO" }
    }

    fn display_line(&self) -> String {
        format!("[{}] (P{}) {}", self.status_label(), self.priority, self.title)
    }
}

fn main() {
    let mut task = Task::new("Write docs", 2);
    println!("{}", task.display_line());
    println!("Debug: {:?}", task);

    task.complete();
    println!("{}", task.display_line());
}
```

Predict the three lines of output, then verify.

## Common Mistakes

**Forgetting to make a variable mutable when calling `&mut self` methods:**

```
error[E0596]: cannot borrow `list` as mutable, as it is not declared as mutable
  --> src/main.rs:5:5
   |
4  |     let list = TaskList::new();
   |         ---- help: consider changing this to be mutable: `mut list`
5  |     list.add("test");
   |     ^^^^ cannot borrow as mutable
```

Fix: add `mut` to the binding -- `let mut list = ...`.

**Using a struct after a method consumed `self`:**

```
error[E0382]: borrow of moved value: `list`
  --> src/main.rs:12:20
   |
3  |     let mut list = TaskList::new();
   |         -------- move occurs because `list` has type `TaskList`
10 |     let s = list.summary();
   |                  --------- `list` moved due to this method call
12 |     println!("{}", list.pending_count());
   |                    ^^^^ value borrowed here after move
```

Fix: either change `summary` to take `&self`, or do not use the struct after calling it.

**Accessing a moved field via struct update syntax:**

```
error[E0382]: borrow of partially moved value: `task_a`
```

Fix: remember that `String` fields are moved during `..` updates. Only `Copy` fields remain accessible on the source.

## Verification

Run each exercise one at a time:

```bash
# Exercise 1 -- basic struct
cargo run

# Exercise 2 -- uncomment the line and observe the error
cargo run

# Exercise 3 -- TaskList domain model
cargo run

# Exercise 4 -- newtype pattern
cargo run
```

Confirm that every exercise compiles (except the intentionally broken lines) and that your predictions match the actual output.

## Summary

- Structs group related data; `impl` blocks attach behavior.
- `&self` reads, `&mut self` mutates, `self` consumes.
- Associated functions (no `self` parameter) act as constructors, called with `Type::name()`.
- Tuple structs enable the newtype pattern for type safety.
- `#[derive(Debug)]` gives you a free `{:?}` formatter.
- Struct update syntax (`..other`) moves non-Copy fields from the source.

## What's Next

Enums and pattern matching -- the other half of Rust's data modeling story. Where structs say "this AND that", enums say "this OR that".

## Resources

- [The Rust Book -- Structs](https://doc.rust-lang.org/book/ch05-00-structs.html)
- [Rust By Example -- Structures](https://doc.rust-lang.org/rust-by-example/custom_types/structs.html)
- [Rust Reference -- Struct types](https://doc.rust-lang.org/reference/types/struct.html)
