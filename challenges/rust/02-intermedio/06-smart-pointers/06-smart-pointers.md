# 6. Smart Pointers

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-ownership-and-borrowing, 02-structs-and-enums
- Comfortable with references (`&T`, `&mut T`) and lifetimes
- Basic understanding of heap vs stack

## Learning Objectives

- Analyze when heap allocation via `Box<T>` is necessary
- Apply `Rc<T>` and `Arc<T>` to model shared ownership
- Implement interior mutability patterns using `RefCell<T>`
- Combine `Rc<RefCell<T>>` to build a mutable tree data structure
- Identify and prevent reference cycles with `Weak<T>`

## Concepts

### Why Smart Pointers?

Regular references borrow data — they never own it. But sometimes you need a pointer that *owns* the data it points to, or you need multiple owners of the same data, or you need to mutate something behind a shared reference. That is what smart pointers solve.

A smart pointer is a struct that acts like a pointer (via the `Deref` trait) and runs cleanup code when dropped (via the `Drop` trait). You have already used two: `String` and `Vec<T>` are both smart pointers that own heap data.

### Box<T> — Heap Allocation

`Box<T>` is the simplest smart pointer. It puts a value on the heap and gives you an owned pointer to it. Three common reasons to reach for `Box`:

1. **Recursive types** — the compiler cannot compute the size of a type that contains itself. A `Box` has a fixed size (one pointer), breaking the recursion.
2. **Large data** — moving a big struct around the stack is expensive. Box it.
3. **Trait objects** — `Box<dyn SomeTrait>` lets you own a value whose concrete type you do not know at compile time.

```rust
// Without Box, this is a compile error: recursive type has infinite size
enum List {
    Cons(i32, Box<List>),
    Nil,
}

fn main() {
    let list = List::Cons(1, Box::new(List::Cons(2, Box::new(List::Nil))));
}
```

### Deref and Deref Coercion

The `Deref` trait lets you customize the behavior of the dereference operator `*`. When you implement `Deref`, the compiler can automatically coerce your smart pointer into a reference to the inner value — this is called *deref coercion*.

```rust
use std::ops::Deref;

struct MyBox<T>(T);

impl<T> MyBox<T> {
    fn new(x: T) -> MyBox<T> {
        MyBox(x)
    }
}

impl<T> Deref for MyBox<T> {
    type Target = T;

    fn deref(&self) -> &T {
        &self.0
    }
}

fn greet(name: &str) {
    println!("Hello, {name}!");
}

fn main() {
    let name = MyBox::new(String::from("Rust"));
    // Deref coercion: &MyBox<String> -> &String -> &str
    greet(&name);
}
```

### Drop — Custom Cleanup

The `Drop` trait lets you run code when a value goes out of scope. Rust calls `drop` automatically — you never call it explicitly. If you need to drop early, use `std::mem::drop`.

```rust
struct DatabaseConn {
    id: u32,
}

impl Drop for DatabaseConn {
    fn drop(&mut self) {
        println!("Closing connection {}", self.id);
    }
}
```

### Rc<T> — Reference Counting

`Rc<T>` (Reference Counted) allows multiple owners of the same heap data. Each call to `Rc::clone` increments the count; when the last `Rc` is dropped, the data is freed.

**Important**: `Rc<T>` is single-threaded only, and the data inside is immutable.

```rust
use std::rc::Rc;

let a = Rc::new(vec![1, 2, 3]);
let b = Rc::clone(&a); // cheap: increments counter, does NOT deep-copy
let c = Rc::clone(&a);

println!("Reference count: {}", Rc::strong_count(&a)); // 3
```

### RefCell<T> — Interior Mutability

`RefCell<T>` moves borrow checking from compile time to runtime. You can get a mutable reference even when the `RefCell` itself is behind a shared reference — but if you violate the rules at runtime, it panics.

```rust
use std::cell::RefCell;

let data = RefCell::new(vec![1, 2, 3]);

// borrow() -> Ref<T>  (like &T)
// borrow_mut() -> RefMut<T>  (like &mut T)
data.borrow_mut().push(4);

println!("{:?}", data.borrow()); // [1, 2, 3, 4]
```

### Rc<RefCell<T>> — Shared Mutable Data

Combine `Rc` (multiple owners) with `RefCell` (interior mutability) to get shared mutable data. This is common for tree and graph structures.

### Arc<T> — Thread-Safe Rc

`Arc<T>` (Atomically Reference Counted) works like `Rc<T>` but is safe to share across threads. The atomic operations make it slightly slower than `Rc`, so only use `Arc` when you actually need thread safety.

### Weak<T> — Breaking Cycles

If two `Rc` values point at each other, neither will ever reach a reference count of zero — memory leak. `Weak<T>` is a non-owning reference that does not increment the strong count. You create one with `Rc::downgrade`, and access the data with `.upgrade()`, which returns `Option<Rc<T>>`.

## Exercises

### Exercise 1: Recursive Types with Box

Complete the binary tree and implement a sum function.

```rust
// src/main.rs

#[derive(Debug)]
enum Tree {
    Leaf(i32),
    // TODO: Add a Node variant that holds a value (i32) and two child Trees.
    // Hint: you need Box to make this work — why?
}

fn sum(tree: &Tree) -> i32 {
    // TODO: recursively sum all values in the tree
    todo!()
}

fn depth(tree: &Tree) -> usize {
    // TODO: return the maximum depth of the tree (a Leaf has depth 1)
    todo!()
}

fn main() {
    let tree = Tree::Node(
        1,
        Box::new(Tree::Node(
            2,
            Box::new(Tree::Leaf(3)),
            Box::new(Tree::Leaf(4)),
        )),
        Box::new(Tree::Leaf(5)),
    );

    assert_eq!(sum(&tree), 15);
    assert_eq!(depth(&tree), 3);
    println!("Sum: {}, Depth: {}", sum(&tree), depth(&tree));
}
```

**Why Box?** Without it, the compiler cannot determine the size of `Tree` at compile time since a `Node` contains two more `Tree` values, which could contain more, infinitely. A `Box<Tree>` is always one pointer wide, giving the compiler a known size.

### Exercise 2: Drop Order and Custom Cleanup

Predict the output before running.

```rust
struct Resource {
    name: String,
}

impl Drop for Resource {
    fn drop(&mut self) {
        println!("Dropping: {}", self.name);
    }
}

fn main() {
    let a = Resource { name: String::from("first") };
    let b = Resource { name: String::from("second") };
    let c = Resource { name: String::from("third") };

    println!("Resources created");

    drop(b); // explicit early drop

    println!("After explicit drop");

    // What order do the remaining resources get dropped?
    // TODO: Write your prediction as comments here, then run to verify.
}
```

### Exercise 3: Shared Ownership with Rc

Model a situation where multiple playlists share the same songs.

```rust
use std::rc::Rc;

#[derive(Debug)]
struct Song {
    title: String,
    artist: String,
}

struct Playlist {
    name: String,
    songs: Vec<Rc<Song>>,
}

impl Playlist {
    fn new(name: &str) -> Self {
        Playlist {
            name: name.to_string(),
            songs: Vec::new(),
        }
    }

    fn add_song(&mut self, song: Rc<Song>) {
        self.songs.push(song);
    }

    fn show(&self) {
        println!("Playlist: {}", self.name);
        for song in &self.songs {
            // TODO: print each song's title and its current reference count
            // Hint: Rc::strong_count takes a reference to the Rc
        }
    }
}

fn main() {
    let bohemian = Rc::new(Song {
        title: "Bohemian Rhapsody".to_string(),
        artist: "Queen".to_string(),
    });
    let stairway = Rc::new(Song {
        title: "Stairway to Heaven".to_string(),
        artist: "Led Zeppelin".to_string(),
    });

    let mut classics = Playlist::new("Classics");
    let mut favorites = Playlist::new("Favorites");

    // TODO: Add bohemian to both playlists, stairway to classics only.
    // Use Rc::clone — not .clone() on Song.

    classics.show();
    favorites.show();

    // TODO: What is the strong_count of bohemian here? Why?
}
```

### Exercise 4: Mutable Tree with Rc<RefCell<T>>

Build a tree where a parent can add children, and children hold a weak reference back to the parent.

```rust
use std::cell::RefCell;
use std::rc::{Rc, Weak};

#[derive(Debug)]
struct Node {
    value: i32,
    children: RefCell<Vec<Rc<Node>>>,
    parent: RefCell<Weak<Node>>,
}

impl Node {
    fn new(value: i32) -> Rc<Node> {
        Rc::new(Node {
            value,
            children: RefCell::new(vec![]),
            parent: RefCell::new(Weak::new()),
        })
    }

    fn add_child(parent: &Rc<Node>, child: &Rc<Node>) {
        // TODO: push the child into the parent's children vec
        // TODO: set the child's parent to a Weak reference to the parent
        // Hint: Rc::downgrade creates a Weak from an Rc
    }
}

fn main() {
    let root = Node::new(1);
    let child_a = Node::new(2);
    let child_b = Node::new(3);
    let grandchild = Node::new(4);

    Node::add_child(&root, &child_a);
    Node::add_child(&root, &child_b);
    Node::add_child(&child_a, &grandchild);

    // Verify parent references work
    // TODO: Starting from grandchild, walk up to the root using .parent
    // and print each node's value. Use .upgrade() on the Weak<Node>.

    // Verify no memory leaks
    println!("root strong count: {}", Rc::strong_count(&root));
    println!("child_a strong count: {}", Rc::strong_count(&child_a));

    // Why would using Rc instead of Weak for parent cause a memory leak?
    // TODO: Write your explanation as a comment.
}
```

### Exercise 5: RefCell Pitfalls

This code compiles but panics at runtime. Find and fix the bug.

```rust
use std::cell::RefCell;

fn main() {
    let data = RefCell::new(vec![1, 2, 3]);

    let borrowed = data.borrow();
    println!("Current: {:?}", borrowed);

    // BUG: This panics. Why?
    data.borrow_mut().push(4);

    println!("Updated: {:?}", data.borrow());
}

// TODO: Fix the code so it compiles AND runs without panicking.
// Hint: think about when `borrowed` is still alive.
```

### Try It Yourself

1. **Doubly linked list**: Implement a simple doubly linked list using `Rc<RefCell<Node>>` for forward links and `Weak<RefCell<Node>>` for backward links. Implement `push_back` and `print_forward` / `print_backward`.

2. **Observer pattern**: Create a struct `EventBus` that holds `Weak` references to subscribers. Subscribers implement a `Listener` trait with a `notify(&self, event: &str)` method. When an event fires, skip any subscribers that have been dropped (`.upgrade()` returns `None`).

3. **Compare performance**: Write a benchmark that clones an `Rc<Vec<u8>>` 1,000,000 times vs cloning an `Arc<Vec<u8>>` 1,000,000 times. Measure the difference — that is the cost of atomic operations.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Using `Rc` across threads | Compile error: `Rc<T>` is not `Send` | Switch to `Arc<T>` |
| Calling `.clone()` on `Rc` contents | Deep copies the inner value | Use `Rc::clone(&x)` explicitly |
| Two active `borrow_mut()` on `RefCell` | Runtime panic | Ensure mutable borrows do not overlap |
| Parent-child both using `Rc` | Memory leak (cycle) | Use `Weak` for the back-reference |
| Forgetting `RefCell` borrows are scoped | Panic from overlapping borrows | Use blocks `{}` to limit borrow scope |

## Verification

After completing all exercises:

```bash
cargo run   # Each exercise as its own main, or combine in one project
```

- Exercise 1: prints `Sum: 15, Depth: 3`
- Exercise 2: drop output matches your prediction
- Exercise 3: reference counts reflect shared ownership correctly
- Exercise 4: walking from grandchild to root prints `4 -> 2 -> 1`
- Exercise 5: no runtime panic

## Summary

| Pointer | Owns data? | Multiple owners? | Mutable? | Thread-safe? |
|---|---|---|---|---|
| `Box<T>` | Yes | No | If you own it | Yes (Send if T: Send) |
| `Rc<T>` | Shared | Yes | No | No |
| `Arc<T>` | Shared | Yes | No | Yes |
| `RefCell<T>` | Yes | No | Yes (runtime checked) | No |
| `Rc<RefCell<T>>` | Shared | Yes | Yes (runtime checked) | No |
| `Weak<T>` | No | N/A | No | Matches Rc/Arc |

The rule of thumb: start with regular references. Reach for `Box` when you need heap allocation or recursive types. Use `Rc`/`Arc` only when shared ownership is genuinely required. Add `RefCell` only when interior mutability is needed. Always prefer compile-time guarantees over runtime checks.

## What's Next

- Exercise 07 covers error handling patterns, where you will see `Box<dyn Error>` as a trait object smart pointer
- Later, concurrency exercises will use `Arc<Mutex<T>>` — the thread-safe version of `Rc<RefCell<T>>`

## Resources

- [The Rust Book, Chapter 15: Smart Pointers](https://doc.rust-lang.org/book/ch15-00-smart-pointers.html)
- [Rust by Example: Box, Rc, Arc](https://doc.rust-lang.org/rust-by-example/std/box.html)
- [std::rc::Rc documentation](https://doc.rust-lang.org/std/rc/struct.Rc.html)
- [std::cell::RefCell documentation](https://doc.rust-lang.org/std/cell/struct.RefCell.html)
