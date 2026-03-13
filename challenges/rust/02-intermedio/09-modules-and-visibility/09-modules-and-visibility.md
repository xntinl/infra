# 9. Modules and Visibility

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-ownership-and-borrowing, 02-structs-and-enums, 08-testing
- Written at least one multi-file Rust project
- Familiar with `use`, `pub`, and basic `mod` declarations

## Learning Objectives

- Apply the Rust 2018+ module path system to organize a multi-file project
- Analyze the differences between `pub`, `pub(crate)`, and `pub(super)` to control API surfaces
- Implement re-exports with `pub use` to create clean public APIs
- Structure a project with nested modules across multiple files

## Concepts

### Why Modules Matter

Every Rust project starts as a single file. That works for 200 lines. By 500 lines, you are scrolling constantly. By 1,000 lines, you are lost. Modules solve this by splitting code into logical units with explicit boundaries and controlled visibility.

But modules are not just about file organization. They are the primary tool for defining your crate's *public API*. Everything is private by default. You choose what to expose, and at what level.

### The mod Keyword

`mod` declares a module. It can be inline or file-based:

```rust
// Inline module
mod math {
    pub fn add(a: i32, b: i32) -> i32 {
        a + b
    }
}

// File-based module — tells the compiler to look for the code elsewhere
mod network; // looks for src/network.rs or src/network/mod.rs
```

### File-Based Modules (2018+ Style)

Before Rust 2018, you had to use `mod.rs` files. The modern approach uses named files:

```
src/
  main.rs          (or lib.rs)
  network.rs       // mod network
  network/
    client.rs      // mod network::client (declared inside network.rs)
    server.rs      // mod network::server
```

In `src/network.rs`:
```rust
pub mod client;
pub mod server;
```

The old style (`src/network/mod.rs`) still works, but the new style is preferred because you never have ten tabs open all named `mod.rs`.

### Visibility Modifiers

| Modifier | Visible to |
|---|---|
| (none) | Same module only |
| `pub` | Everyone (the whole world) |
| `pub(crate)` | Anything in the same crate |
| `pub(super)` | The parent module |
| `pub(in path)` | A specific ancestor module |

```rust
mod outer {
    pub(crate) fn crate_wide() {} // visible within the crate

    mod inner {
        pub(super) fn parent_only() {} // visible only to `outer`

        fn truly_private() {} // visible only to `inner`
    }

    fn use_inner() {
        inner::parent_only(); // works
    }
}

fn main() {
    outer::crate_wide(); // works
    // outer::inner::parent_only(); // ERROR: not visible here
}
```

### Paths: Absolute vs Relative

```rust
mod garden {
    pub mod flowers {
        pub fn bloom() {}
    }
}

// Absolute path (from crate root)
crate::garden::flowers::bloom();

// Relative path (from current module)
garden::flowers::bloom();

// self refers to the current module
self::some_function();

// super refers to the parent module
super::parent_function();
```

### use Statements

`use` brings a path into scope to avoid repeating long paths:

```rust
use std::collections::HashMap;
use std::io::{self, Read, Write}; // multiple items + the module itself

// Renaming to avoid conflicts
use std::fmt::Result as FmtResult;
use std::io::Result as IoResult;
```

### Re-exports with pub use

`pub use` re-exports an item from a different location. This is how you create a clean public API that hides your internal module structure:

```rust
// src/lib.rs
mod internal {
    pub mod parser {
        pub fn parse(input: &str) -> Vec<String> {
            input.split(',').map(|s| s.trim().to_string()).collect()
        }
    }
}

// Users see `my_crate::parse`, not `my_crate::internal::parser::parse`
pub use internal::parser::parse;
```

## Exercises

### Exercise 1: Basic Module Structure

Create this project structure and make it compile.

```
modules-exercise/
  src/
    main.rs
    math.rs
    math/
      geometry.rs
  Cargo.toml
```

```rust
// src/math.rs
pub mod geometry;

pub fn add(a: f64, b: f64) -> f64 {
    a + b
}

// This function should only be visible within the crate, not to external users
// TODO: What visibility modifier do you need?
fn internal_round(value: f64, decimals: u32) -> f64 {
    let factor = 10_f64.powi(decimals as i32);
    (value * factor).round() / factor
}
```

```rust
// src/math/geometry.rs

// TODO: Implement these functions with correct visibility.
// `area_circle` and `area_rectangle` should be public.
// `validate_positive` is a helper — only this module should see it.

fn validate_positive(value: f64) -> Result<f64, String> {
    if value <= 0.0 {
        Err(format!("{value} must be positive"))
    } else {
        Ok(value)
    }
}

fn area_circle(radius: f64) -> Result<f64, String> {
    let r = validate_positive(radius)?;
    Ok(std::f64::consts::PI * r * r)
}

fn area_rectangle(width: f64, height: f64) -> Result<f64, String> {
    let w = validate_positive(width)?;
    let h = validate_positive(height)?;
    Ok(w * h)
}
```

```rust
// src/main.rs
mod math;

fn main() {
    println!("2 + 3 = {}", math::add(2.0, 3.0));
    println!("Circle area: {}", math::geometry::area_circle(5.0).unwrap());
    println!("Rect area: {}", math::geometry::area_rectangle(4.0, 6.0).unwrap());

    // This should NOT compile — uncomment to verify:
    // math::internal_round(3.14159, 2);
    // math::geometry::validate_positive(5.0);
}
```

### Exercise 2: Visibility Levels

Predict which lines compile and which do not. Then verify.

```rust
// src/main.rs (replace Exercise 1's main.rs)

mod api {
    pub struct Request {
        pub path: String,
        pub(crate) method: String,    // visible within crate only
        pub(super) headers: Vec<String>, // visible to parent (main module)
        body: Option<String>,         // private
    }

    impl Request {
        pub fn new(path: &str, method: &str) -> Self {
            Request {
                path: path.to_string(),
                method: method.to_string(),
                headers: vec![],
                body: None,
            }
        }

        pub(crate) fn set_body(&mut self, body: String) {
            self.body = Some(body);
        }
    }

    pub mod middleware {
        pub fn log_request(req: &super::Request) {
            println!("Request to: {}", req.path);
            // TODO: Which of these fields can middleware access?
            // Try each one and predict before compiling:
            // println!("Method: {}", req.method);   // ?
            // println!("Headers: {:?}", req.headers); // ?
            // println!("Body: {:?}", req.body);       // ?
        }
    }
}

fn main() {
    let mut req = api::Request::new("/users", "GET");

    // TODO: Predict which of these compile. Mark each Y or N before running.
    println!("{}", req.path);          // ?
    println!("{}", req.method);        // ?
    println!("{:?}", req.headers);     // ?
    // println!("{:?}", req.body);     // ?

    req.set_body("hello".to_string()); // ?
}
```

### Exercise 3: Re-exports for a Clean API

You have messy internals. Create a clean public surface.

```rust
// src/lib.rs

// Internal structure (complex, nested)
mod storage {
    pub mod backends {
        pub mod memory {
            pub struct MemoryStore {
                data: std::collections::HashMap<String, String>,
            }

            impl MemoryStore {
                pub fn new() -> Self {
                    MemoryStore {
                        data: std::collections::HashMap::new(),
                    }
                }

                pub fn get(&self, key: &str) -> Option<&String> {
                    self.data.get(key)
                }

                pub fn set(&mut self, key: String, value: String) {
                    self.data.insert(key, value);
                }
            }
        }
    }

    pub mod error {
        #[derive(Debug)]
        pub enum StoreError {
            NotFound(String),
            ConnectionFailed(String),
        }

        impl std::fmt::Display for StoreError {
            fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
                match self {
                    StoreError::NotFound(k) => write!(f, "key not found: {k}"),
                    StoreError::ConnectionFailed(m) => write!(f, "connection failed: {m}"),
                }
            }
        }

        impl std::error::Error for StoreError {}
    }
}

// TODO: Add pub use statements so that users of this crate can write:
//   use my_crate::MemoryStore;
//   use my_crate::StoreError;
// instead of:
//   use my_crate::storage::backends::memory::MemoryStore;
//   use my_crate::storage::error::StoreError;

// Also re-export a "prelude" module for convenience:
// pub mod prelude {
//     pub use super::...;
// }
```

### Exercise 4: Multi-File Project

Build this structure from scratch. The goal is a small task manager library.

```
task-manager/
  src/
    lib.rs
    task.rs
    store.rs
    store/
      memory.rs
  Cargo.toml
```

```rust
// src/task.rs
// TODO: Define a Task struct with:
//   - id: u64 (pub)
//   - title: String (pub)
//   - completed: bool (pub(crate) — internal state, not directly settable)
// Implement:
//   - pub fn new(id: u64, title: &str) -> Task
//   - pub fn complete(&mut self)
//   - pub fn is_completed(&self) -> bool
```

```rust
// src/store.rs
pub mod memory;

// TODO: Define a trait `TaskStore` with:
//   - fn add(&mut self, task: super::task::Task)
//   - fn get(&self, id: u64) -> Option<&super::task::Task>
//   - fn list_incomplete(&self) -> Vec<&super::task::Task>
```

```rust
// src/store/memory.rs
use crate::task::Task;
use super::TaskStore;

// TODO: Implement an InMemoryStore struct that implements TaskStore.
// Use a Vec<Task> internally.
```

```rust
// src/lib.rs
pub mod task;
pub mod store;

// TODO: Add re-exports so users can write:
//   use task_manager::{Task, InMemoryStore, TaskStore};
```

### Exercise 5: super, self, and crate Paths

Fix the path errors in this code. Each function has a comment indicating the fix.

```rust
mod config {
    pub const MAX_RETRIES: u32 = 3;

    pub mod database {
        pub fn connection_string() -> String {
            format!("retries={}", MAX_RETRIES) // ERROR: can't find MAX_RETRIES
            // TODO: Fix using the correct path keyword
        }

        pub mod pool {
            pub fn max_connections() -> u32 {
                // Needs to call database::connection_string (the parent)
                let _conn = connection_string(); // ERROR
                // TODO: Fix using the correct path keyword
                10
            }

            pub fn retry_limit() -> u32 {
                // Needs config::MAX_RETRIES (the grandparent's constant)
                MAX_RETRIES // ERROR
                // TODO: Fix using the correct path keyword
            }
        }
    }

    pub mod cache {
        pub fn default_ttl() -> u64 {
            // Needs to reference a sibling module's function
            let _conn = database::connection_string(); // ERROR
            // TODO: Fix — cache and database are siblings under config
            3600
        }
    }
}

fn main() {
    println!("{}", config::database::connection_string());
    println!("{}", config::database::pool::max_connections());
    println!("{}", config::database::pool::retry_limit());
    println!("{}", config::cache::default_ttl());
}
```

### Try It Yourself

1. **Facade pattern**: Create a module `facade` that re-exports items from three internal modules, exposing only a curated set of types and functions. Verify that internal details cannot be accessed from `main`.

2. **Orphan rule exploration**: Try implementing a foreign trait on a foreign type (e.g., `Display` for `Vec<i32>`). Observe the error. Then use the newtype pattern (a wrapper struct in your module) to work around it.

3. **Workspace modules**: Create a cargo workspace with two crates: a library (`core`) and a binary (`cli`). The binary depends on the library. Observe how visibility works across crate boundaries — `pub(crate)` items in the library are invisible to the binary.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Forgetting `pub` on submodule items | "function is private" error | Add `pub` to the function and every `mod` in the chain |
| Declaring `mod foo;` but no `foo.rs` file | "file not found" error | Create the file, check spelling |
| Using `mod.rs` AND named file for same module | Compile error: duplicate module | Pick one approach, not both |
| Circular module dependencies | Compile error | Restructure to remove the cycle |
| `pub use` without `pub mod` | Item not accessible | Ensure the module itself is `pub` or the re-export path is valid |
| Confusing `use` with `mod` | Module not loaded | `mod` loads the module, `use` brings paths into scope |

## Verification

- Exercise 1: Compiles and runs. Uncommenting the private-access lines produces errors.
- Exercise 2: Your predictions match the compiler's verdict.
- Exercise 3: External code can use short paths like `my_crate::MemoryStore`.
- Exercise 4: `cargo test` passes for the task manager with tests in each module.
- Exercise 5: All path errors resolved. The code compiles and prints correct output.

## Summary

The module system has three parts that work together:

1. **`mod`** declares a module and tells the compiler where to find its code.
2. **Visibility modifiers** (`pub`, `pub(crate)`, `pub(super)`) control who can see what.
3. **`use` and `pub use`** bring paths into scope and reshape the public API.

The key insight: your internal file structure does not need to match your public API. Use `pub use` to create a clean surface while keeping your internals organized however makes sense.

## What's Next

- Exercise 10 covers Cargo and dependencies, including workspaces (multiple crates organized as modules)
- Later exercises on traits and generics build on visibility to define clean trait-based APIs

## Resources

- [The Rust Book, Chapter 7: Packages, Crates, and Modules](https://doc.rust-lang.org/book/ch07-00-managing-growing-projects-with-packages-crates-and-modules.html)
- [Rust Reference: Visibility and Privacy](https://doc.rust-lang.org/reference/visibility-and-privacy.html)
- [Rust API Guidelines: Re-exports](https://rust-lang.github.io/api-guidelines/necessities.html)
