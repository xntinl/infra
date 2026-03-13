# 10. Advanced Traits

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-09 (ownership, lifetimes, basic traits, error handling, unsafe)
- Solid understanding of generics, trait bounds, and dynamic dispatch at an introductory level
- Familiarity with `Box<dyn Trait>` and when you have reached for it

## Learning Objectives

- Distinguish associated types from generic type parameters and choose correctly
- Apply fully qualified syntax to disambiguate overlapping method names
- Evaluate object safety rules and design traits that support dynamic dispatch
- Analyze the performance trade-off between monomorphization and vtable dispatch
- Implement blanket implementations, marker traits, extension traits, and sealed traits

## Concepts

### Associated Types vs Generic Parameters

A trait with a generic parameter can be implemented multiple times for the same type. A trait with an associated type can be implemented exactly once:

```rust
// Generic parameter: multiple impls per type
trait Convert<T> {
    fn convert(&self) -> T;
}

impl Convert<String> for i32 {
    fn convert(&self) -> String { self.to_string() }
}

impl Convert<f64> for i32 {
    fn convert(&self) -> f64 { *self as f64 }
}

// Associated type: one impl per type
trait IntoOutput {
    type Output;
    fn into_output(self) -> Self::Output;
}

impl IntoOutput for i32 {
    type Output = String;
    fn into_output(self) -> String { self.to_string() }
}
```

**Rule of thumb**: if the relationship is "a type has exactly one natural output/item/error", use an associated type (`Iterator::Item`, `Add::Output`). If the relationship is "a type can be converted to many things", use a generic parameter (`From<T>`, `AsRef<T>`).

### Fully Qualified Syntax

When a type implements multiple traits with the same method name, or has an inherent method that shadows a trait method:

```rust
trait Pilot {
    fn fly(&self);
}

trait Wizard {
    fn fly(&self);
}

struct Human;

impl Pilot for Human {
    fn fly(&self) { println!("captain speaking"); }
}

impl Wizard for Human {
    fn fly(&self) { println!("levitation"); }
}

impl Human {
    fn fly(&self) { println!("flapping arms"); }
}

fn main() {
    let h = Human;
    h.fly();               // inherent method: "flapping arms"
    Pilot::fly(&h);        // "captain speaking"
    Wizard::fly(&h);       // "levitation"

    // For associated functions (no self), you need the full turbofish form:
    // <Type as Trait>::function()
    <Human as Pilot>::fly(&h);
}
```

### Trait Objects and Object Safety

A trait is **object-safe** (and thus usable as `dyn Trait`) if all of its methods:

1. Do not return `Self` (by value)
2. Do not have generic type parameters
3. Have a receiver (`self`, `&self`, `&mut self`, `Box<Self>`, etc.)

Additional rules: the trait must not require `Self: Sized`, and any supertraits must also be object-safe.

```rust
// Object-safe
trait Draw {
    fn draw(&self);
}

// NOT object-safe: returns Self
trait Cloneable {
    fn clone_self(&self) -> Self;
}

// NOT object-safe: generic method
trait Serialize {
    fn serialize<W: std::io::Write>(&self, writer: &mut W);
}

// Workaround: add a Sized bound to opt specific methods out of the vtable
trait Mixed: Sized {
    fn safe_method(&self) {}       // in vtable

    fn generic_method<T>(&self) where Self: Sized {}  // excluded from vtable
}
// But now `dyn Mixed` itself is not possible because the supertrait is `Sized`.
// Better pattern: put the Sized bound only on the method, not the trait:
trait BetterMixed {
    fn safe_method(&self) {}
    fn generic_method<T>(&self) where Self: Sized {}
}
// Now `dyn BetterMixed` works, but generic_method is not callable on trait objects.
```

### Static vs Dynamic Dispatch

**Monomorphization (static dispatch):** the compiler generates a separate copy of the function for each concrete type. Zero overhead at runtime, but increases binary size.

```rust
fn print_it<T: std::fmt::Display>(val: &T) {
    println!("{val}");
}
// Compiler generates: print_it::<i32>, print_it::<String>, etc.
```

**Vtable (dynamic dispatch):** one copy of the function, dispatched through a fat pointer (data ptr + vtable ptr). Smaller binary, but each method call is an indirect jump and the compiler cannot inline.

```rust
fn print_it(val: &dyn std::fmt::Display) {
    println!("{val}");
}
// One function body. Method resolution through vtable at runtime.
```

**When to use which:**

| Static dispatch | Dynamic dispatch |
|---|---|
| Hot paths, tight loops | Heterogeneous collections |
| When you need inlining | Plugin architectures |
| Compile-time-known types | Reducing compile times (fewer monomorphized copies) |
| Library APIs with `impl Trait` | Trait objects behind `Box<dyn Trait>` |

### Blanket Implementations

A blanket impl provides a trait for all types satisfying some bound:

```rust
// From the standard library:
impl<T: Display> ToString for T {
    fn to_string(&self) -> String {
        format!("{self}")
    }
}
```

This is why you never implement `ToString` directly -- implement `Display` and you get it for free. Blanket impls are powerful but create orphan-rule constraints: once a blanket impl exists, nobody (including downstream crates) can provide a more specific impl.

### Marker Traits

Traits with no methods that carry semantic meaning for the compiler or other generic code:

```rust
// Standard library markers
// Send: safe to transfer between threads
// Sync: safe to share (&T) between threads
// Sized: has a known size at compile time
// Unpin: can be moved after being pinned
// Copy: can be duplicated via bitwise copy

// Custom marker
trait Auditable {}

fn process<T: Auditable>(item: T) {
    // We know T has been marked as auditable by its author
}
```

### Extension Traits

Add methods to types you do not own, without orphan rule violations, by defining a new trait and providing a blanket impl:

```rust
trait StrExt {
    fn is_blank(&self) -> bool;
}

impl StrExt for str {
    fn is_blank(&self) -> bool {
        self.trim().is_empty()
    }
}

// Now any &str gets .is_blank()
assert!("   ".is_blank());
```

Common in the ecosystem: `itertools::Itertools`, `futures::StreamExt`, `tokio::io::AsyncReadExt`.

### Sealed Traits

Prevent downstream crates from implementing your trait. This lets you add methods in future versions without breaking changes:

```rust
mod private {
    pub trait Sealed {}
}

pub trait MyApi: private::Sealed {
    fn do_thing(&self);
    // You can add methods later because no external type implements this.
}

pub struct TypeA;
impl private::Sealed for TypeA {}
impl MyApi for TypeA {
    fn do_thing(&self) { println!("A"); }
}
```

External crates can call methods on `MyApi` but cannot implement it because `private::Sealed` is not reachable.

## Exercises

### Exercise 1: Graph Processing with Associated Types

Design a `Graph` trait with associated types for `Node` and `Edge`. Implement it for both an adjacency list and an adjacency matrix. Write a generic `shortest_path` function that works with any `Graph` implementation.

The trait should support:
- Adding nodes and edges
- Querying neighbors
- A generic BFS shortest path that returns `Option<Vec<Self::Node>>`

**Hints:**
- `Node` needs `Clone + Eq + Hash` bounds on the associated type
- `Edge` might carry a weight
- Think about why associated types are correct here (a graph has one node type, not many)

**Cargo.toml:**
```toml
[package]
name = "advanced-traits"
edition = "2021"
```

<details>
<summary>Solution</summary>

```rust
use std::collections::{HashMap, HashSet, VecDeque};
use std::hash::Hash;

trait Graph {
    type Node: Clone + Eq + Hash;
    type Edge: Clone;

    fn add_node(&mut self, node: Self::Node);
    fn add_edge(&mut self, from: Self::Node, to: Self::Node, edge: Self::Edge);
    fn neighbors(&self, node: &Self::Node) -> Vec<(Self::Node, Self::Edge)>;

    fn bfs_path(&self, start: &Self::Node, goal: &Self::Node) -> Option<Vec<Self::Node>> {
        let mut visited: HashSet<Self::Node> = HashSet::new();
        let mut queue: VecDeque<Vec<Self::Node>> = VecDeque::new();
        queue.push_back(vec![start.clone()]);
        visited.insert(start.clone());

        while let Some(path) = queue.pop_front() {
            let current = path.last().unwrap();
            if current == goal {
                return Some(path);
            }
            for (neighbor, _edge) in self.neighbors(current) {
                if visited.insert(neighbor.clone()) {
                    let mut new_path = path.clone();
                    new_path.push(neighbor);
                    queue.push_back(new_path);
                }
            }
        }
        None
    }
}

// --- Adjacency List Implementation ---

struct AdjList<N: Eq + Hash + Clone, E: Clone> {
    edges: HashMap<N, Vec<(N, E)>>,
}

impl<N: Eq + Hash + Clone, E: Clone> AdjList<N, E> {
    fn new() -> Self {
        Self { edges: HashMap::new() }
    }
}

impl<N: Eq + Hash + Clone, E: Clone> Graph for AdjList<N, E> {
    type Node = N;
    type Edge = E;

    fn add_node(&mut self, node: N) {
        self.edges.entry(node).or_default();
    }

    fn add_edge(&mut self, from: N, to: N, edge: E) {
        self.edges.entry(from.clone()).or_default().push((to.clone(), edge.clone()));
        self.edges.entry(to).or_default().push((from, edge));
    }

    fn neighbors(&self, node: &N) -> Vec<(N, E)> {
        self.edges.get(node).cloned().unwrap_or_default()
    }
}

// --- Generic function that works with any Graph ---

fn print_path<G: Graph>(graph: &G, start: &G::Node, goal: &G::Node)
where
    G::Node: std::fmt::Debug,
{
    match graph.bfs_path(start, goal) {
        Some(path) => println!("Path: {path:?}"),
        None => println!("No path found"),
    }
}

fn main() {
    let mut g = AdjList::new();
    g.add_edge("a", "b", 1);
    g.add_edge("b", "c", 1);
    g.add_edge("a", "c", 1);
    g.add_edge("c", "d", 1);

    print_path(&g, &"a", &"d");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bfs_finds_shortest() {
        let mut g = AdjList::new();
        g.add_edge(0, 1, ());
        g.add_edge(0, 2, ());
        g.add_edge(1, 3, ());
        g.add_edge(2, 3, ());

        let path = g.bfs_path(&0, &3).unwrap();
        assert_eq!(path.len(), 3); // 0 -> 1 -> 3 or 0 -> 2 -> 3
    }

    #[test]
    fn no_path() {
        let mut g: AdjList<i32, ()> = AdjList::new();
        g.add_node(0);
        g.add_node(1);
        assert!(g.bfs_path(&0, &1).is_none());
    }
}
```

**Trade-off analysis:** Associated types enforce one node/edge type per graph, which is correct -- a single graph does not mix `String` nodes and `i32` nodes. If you used generic parameters (`trait Graph<N, E>`), you could implement `Graph<String, f64>` and `Graph<i32, ()>` for the same struct, which makes no semantic sense for a single graph instance.
</details>

### Exercise 2: Heterogeneous Plugin System

Build a plugin system where plugins are stored as `Vec<Box<dyn Plugin>>`. Each plugin has a name, a priority, and an `execute` method. Write a `PluginRegistry` that sorts plugins by priority and executes them in order.

Then try to add a `clone` method to `Plugin`. Observe why it breaks object safety. Fix it using the `dyn-clone` pattern (manual vtable trick).

**Hints:**
- `Clone` is not object-safe because `clone()` returns `Self`
- The workaround: add a `fn clone_box(&self) -> Box<dyn Plugin>` method
- Implement `Clone` for `Box<dyn Plugin>` using `clone_box`

<details>
<summary>Solution</summary>

```rust
trait Plugin: PluginClone {
    fn name(&self) -> &str;
    fn priority(&self) -> u32;
    fn execute(&self, input: &str) -> String;
}

// Separate trait for the clone workaround
trait PluginClone {
    fn clone_box(&self) -> Box<dyn Plugin>;
}

impl<T: Plugin + Clone + 'static> PluginClone for T {
    fn clone_box(&self) -> Box<dyn Plugin> {
        Box::new(self.clone())
    }
}

impl Clone for Box<dyn Plugin> {
    fn clone(&self) -> Self {
        self.clone_box()
    }
}

struct PluginRegistry {
    plugins: Vec<Box<dyn Plugin>>,
}

impl PluginRegistry {
    fn new() -> Self {
        Self { plugins: Vec::new() }
    }

    fn register(&mut self, plugin: Box<dyn Plugin>) {
        self.plugins.push(plugin);
        self.plugins.sort_by_key(|p| std::cmp::Reverse(p.priority()));
    }

    fn execute_all(&self, input: &str) -> Vec<String> {
        self.plugins.iter().map(|p| {
            println!("[{}] executing (priority {})", p.name(), p.priority());
            p.execute(input)
        }).collect()
    }
}

// --- Concrete Plugins ---

#[derive(Clone)]
struct UpperPlugin;

impl Plugin for UpperPlugin {
    fn name(&self) -> &str { "upper" }
    fn priority(&self) -> u32 { 10 }
    fn execute(&self, input: &str) -> String { input.to_uppercase() }
}

#[derive(Clone)]
struct PrefixPlugin {
    prefix: String,
}

impl Plugin for PrefixPlugin {
    fn name(&self) -> &str { "prefix" }
    fn priority(&self) -> u32 { 20 }
    fn execute(&self, input: &str) -> String {
        format!("{}{}", self.prefix, input)
    }
}

fn main() {
    let mut registry = PluginRegistry::new();
    registry.register(Box::new(UpperPlugin));
    registry.register(Box::new(PrefixPlugin { prefix: ">> ".into() }));

    let results = registry.execute_all("hello world");
    for r in &results {
        println!("  -> {r}");
    }

    // Clone the entire registry
    let cloned = registry.plugins.clone();
    assert_eq!(cloned.len(), 2);
    println!("cloned registry has {} plugins", cloned.len());
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn priority_ordering() {
        let mut reg = PluginRegistry::new();
        reg.register(Box::new(UpperPlugin));       // priority 10
        reg.register(Box::new(PrefixPlugin { prefix: "x".into() })); // priority 20
        assert_eq!(reg.plugins[0].name(), "prefix"); // higher priority first
    }

    #[test]
    fn clone_box_works() {
        let p: Box<dyn Plugin> = Box::new(UpperPlugin);
        let p2 = p.clone();
        assert_eq!(p.execute("hi"), p2.execute("hi"));
    }
}
```

**Why this matters:** The `PluginClone` trick is the standard pattern when you need cloneable trait objects. The `dyn-clone` crate automates it. Understanding the underlying mechanism (blanket impl on `T: Clone + 'static` that returns `Box::new(self.clone())`) teaches you how vtables compose.
</details>

### Exercise 3: Extension Trait + Sealed Trait

Create a sealed `ResultExt` extension trait that adds methods to `Result<T, E>`:
- `log_err(self, msg: &str) -> Self` -- logs the error (via `eprintln!`) and returns self unchanged
- `map_err_to<F: From<E>>(self) -> Result<T, F>` -- shorthand for `map_err(Into::into)`

The trait must be sealed so downstream users cannot add implementations, only call the methods.

**Hints:**
- The sealed pattern uses a `mod private { pub trait Sealed {} }` with blanket impl
- Implement `Sealed` for `Result<T, E>` generically so all Results are covered
- This is how `anyhow`, `color-eyre`, and `tokio` expose their extension traits

<details>
<summary>Solution</summary>

```rust
mod private {
    pub trait Sealed {}
    impl<T, E> Sealed for Result<T, E> {}
}

pub trait ResultExt<T, E>: private::Sealed {
    fn log_err(self, msg: &str) -> Self;
    fn map_err_to<F: From<E>>(self) -> Result<T, F>;
}

impl<T, E: std::fmt::Display> ResultExt<T, E> for Result<T, E> {
    fn log_err(self, msg: &str) -> Self {
        if let Err(ref e) = self {
            eprintln!("[ERROR] {msg}: {e}");
        }
        self
    }

    fn map_err_to<F: From<E>>(self) -> Result<T, F> {
        self.map_err(Into::into)
    }
}

// --- Usage ---

#[derive(Debug)]
enum AppError {
    Io(std::io::Error),
    Parse(std::num::ParseIntError),
}

impl From<std::io::Error> for AppError {
    fn from(e: std::io::Error) -> Self { AppError::Io(e) }
}

impl From<std::num::ParseIntError> for AppError {
    fn from(e: std::num::ParseIntError) -> Self { AppError::Parse(e) }
}

fn parse_port(s: &str) -> Result<u16, AppError> {
    s.parse::<u16>()
        .log_err("failed to parse port")
        .map_err_to()
}

fn main() {
    match parse_port("8080") {
        Ok(port) => println!("port: {port}"),
        Err(e) => println!("error: {e:?}"),
    }

    match parse_port("not_a_number") {
        Ok(port) => println!("port: {port}"),
        Err(e) => println!("error: {e:?}"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn log_err_passes_through_ok() {
        let r: Result<i32, String> = Ok(42);
        assert_eq!(r.log_err("should not print").unwrap(), 42);
    }

    #[test]
    fn log_err_passes_through_err() {
        let r: Result<i32, String> = Err("boom".into());
        assert!(r.log_err("should print").is_err());
    }

    #[test]
    fn map_err_to_converts() {
        let r: Result<(), std::num::ParseIntError> = "nope".parse::<i32>().map(|_| ());
        let converted: Result<(), AppError> = r.map_err_to();
        assert!(matches!(converted, Err(AppError::Parse(_))));
    }
}
```

**Sealed trait trade-off:** You gain the freedom to add methods in semver-compatible releases. You lose the ability for downstream crates to implement the trait for their own types. For extension traits on standard library types, sealing is almost always correct.
</details>

## Common Mistakes

1. **Using generic params when an associated type is appropriate.** If you find yourself writing `fn process<C: Collection<Item=String>>(c: C)` and `Item` is always uniquely determined, make it an associated type.

2. **Forgetting object safety constraints.** Adding a single `fn clone(&self) -> Self` to a trait makes all existing `dyn Trait` usage fail. Design trait objects from the start.

3. **Overusing dynamic dispatch.** `Box<dyn Trait>` has real overhead: indirect calls, no inlining, heap allocation. Use `enum_dispatch` or enums for closed sets of types.

4. **Blanket impl conflicts.** `impl<T: Display> MyTrait for T` conflicts with any specific impl of `MyTrait`. Plan your impl strategy before writing blanket impls.

5. **Not sealing public traits.** If you do not want downstream implementations, seal the trait. Adding a method later is a breaking change if external types implement the trait.

## Verification

- All exercises should pass `cargo test`
- `cargo clippy -- -W clippy::all` should produce no warnings
- Try adding an external impl of the sealed trait to confirm the compiler rejects it

## Summary

Advanced traits are the backbone of Rust's zero-cost abstraction philosophy. Associated types pin down relationships, fully qualified syntax resolves ambiguity, object safety rules define the boundary of dynamic dispatch, and patterns like blanket impls, extension traits, and sealed traits give you fine-grained control over your public API surface.

## What's Next

Exercise 11 tackles advanced lifetimes -- the type-system dimension that interacts most deeply with traits, especially in higher-ranked trait bounds and GATs.

## Resources

- [The Rust Reference: Traits](https://doc.rust-lang.org/reference/items/traits.html)
- [Object Safety RFC](https://rust-lang.github.io/rfcs/0255-object-safety.html)
- [dyn-clone crate](https://docs.rs/dyn-clone)
- [enum_dispatch crate](https://docs.rs/enum_dispatch) -- static dispatch for closed type sets
- [Rust API Guidelines: Sealed traits](https://rust-lang.github.io/api-guidelines/future-proofing.html)
