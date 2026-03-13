# 26. Trait Objects and Dynamic Dispatch

**Difficulty**: Intermedio

## Prerequisites
- Completed: traits, generics, smart pointers (Box, Rc)
- Comfortable with trait bounds and impl Trait syntax
- Understanding of stack vs heap allocation

## Learning Objectives
After completing this exercise, you will be able to:
- Use `dyn Trait` to create trait objects for runtime polymorphism
- Understand vtable layout and the cost of dynamic dispatch
- Distinguish `&dyn Trait` from `Box<dyn Trait>` and choose the right one
- Apply object safety rules to decide which traits can become trait objects
- Build heterogeneous collections with trait objects
- Downcast trait objects using `Any`
- Choose between generics (static dispatch) and trait objects (dynamic dispatch)

## Concepts

### What Is Dynamic Dispatch?

When you use generics with trait bounds, the compiler generates specialized code for each concrete type at compile time. This is **static dispatch** — the exact function to call is known before the program runs.

Sometimes you need a collection of values with different concrete types that all share a trait. You cannot use generics here because a `Vec<T>` holds values of one single type `T`. This is where **trait objects** come in.

A trait object (`dyn Trait`) tells the compiler: "I don't know the concrete type at compile time — look it up at runtime through a vtable."

```rust
trait Draw {
    fn draw(&self);
}

struct Circle { radius: f64 }
struct Square { side: f64 }

impl Draw for Circle {
    fn draw(&self) { println!("Drawing circle r={}", self.radius); }
}
impl Draw for Square {
    fn draw(&self) { println!("Drawing square s={}", self.side); }
}

// Heterogeneous collection — different concrete types, one trait
let shapes: Vec<Box<dyn Draw>> = vec![
    Box::new(Circle { radius: 1.0 }),
    Box::new(Square { side: 2.0 }),
];

for shape in &shapes {
    shape.draw(); // dynamic dispatch — vtable lookup at runtime
}
```

### Vtable Layout

A trait object is a **fat pointer** — two machine words:

1. **Data pointer**: points to the concrete value on the heap (or stack, for `&dyn`).
2. **Vtable pointer**: points to a table of function pointers for the trait methods.

```text
Box<dyn Draw> layout (16 bytes on 64-bit):
┌────────────────────┬────────────────────┐
│   data pointer     │   vtable pointer   │
│  (points to value) │ (points to vtable) │
└────────────────────┴────────────────────┘

Vtable for Circle's impl of Draw:
┌──────────────┬─────────────┬──────────────┐
│ drop_in_place│   size      │  draw fn ptr │
└──────────────┴─────────────┴──────────────┘
```

Each call through `dyn Draw` means an indirect function call through the vtable. This is typically a few nanoseconds of overhead — negligible for most applications, but relevant in hot loops or latency-critical code.

### `&dyn Trait` vs `Box<dyn Trait>`

| Form | Ownership | Allocation | Use when |
|------|-----------|------------|----------|
| `&dyn Trait` | Borrowed | None (borrows existing data) | Passing references temporarily |
| `Box<dyn Trait>` | Owned | Heap | Storing in collections, returning from functions |
| `Rc<dyn Trait>` | Shared | Heap + ref count | Multiple owners, single thread |

```rust
// Borrowed — no allocation
fn print_shape(shape: &dyn Draw) {
    shape.draw();
}

// Owned — caller gives up ownership
fn store_shape(shape: Box<dyn Draw>) -> Box<dyn Draw> {
    shape
}
```

### Object Safety Rules

Not every trait can be used as `dyn Trait`. A trait is **object-safe** if:

1. All methods have a receiver (`self`, `&self`, `&mut self`, `self: Box<Self>`, etc.).
2. No method returns `Self` (the concrete type is erased, so the compiler cannot know its size).
3. No method has generic type parameters (generics require monomorphization, which contradicts dynamic dispatch).
4. The trait does not require `Self: Sized`.

```rust
// Object-safe
trait Draw {
    fn draw(&self);
}

// NOT object-safe — returns Self
trait Clonable {
    fn clone_self(&self) -> Self;
}

// NOT object-safe — generic method
trait Serialize {
    fn serialize<W: std::io::Write>(&self, writer: &mut W);
}
```

**Workaround for Clone**: The standard library's `Clone` is not object-safe, but you can define a helper:

```rust
trait CloneDraw: Draw {
    fn clone_box(&self) -> Box<dyn Draw>;
}

impl<T: Draw + Clone + 'static> CloneDraw for T {
    fn clone_box(&self) -> Box<dyn Draw> {
        Box::new(self.clone())
    }
}
```

### Downcasting with `Any`

Once you erase the concrete type, you can recover it using `std::any::Any`:

```rust
use std::any::Any;

trait Plugin: Any {
    fn name(&self) -> &str;
    fn as_any(&self) -> &dyn Any;
}

struct Logger;
impl Plugin for Logger {
    fn name(&self) -> &str { "logger" }
    fn as_any(&self) -> &dyn Any { self }
}

fn downcast_example(plugin: &dyn Plugin) {
    if let Some(logger) = plugin.as_any().downcast_ref::<Logger>() {
        println!("Found Logger plugin!");
    }
}
```

### Generics vs Trait Objects — When to Use Each

| Criterion | Generics (static) | Trait objects (dynamic) |
|-----------|-------------------|------------------------|
| Performance | Zero-cost, inlined | Vtable indirection |
| Binary size | Larger (monomorphized copies) | Smaller (single function body) |
| Heterogeneous collections | Not possible | Natural fit |
| Compile times | Slower (more code generated) | Faster |
| API ergonomics | Caller chooses type | Callee hides type |

**Rule of thumb**: use generics by default. Reach for `dyn Trait` when you need heterogeneous collections, plugin architectures, or want to reduce binary size.

## Exercises

### Exercise 1: Heterogeneous Collection

Build a simple drawing canvas that stores shapes of different types.

```rust
use std::fmt;

trait Shape: fmt::Display {
    fn area(&self) -> f64;
    fn perimeter(&self) -> f64;
}

struct Circle {
    radius: f64,
}

struct Rectangle {
    width: f64,
    height: f64,
}

struct Triangle {
    a: f64,
    b: f64,
    c: f64,
}

// TODO: Implement fmt::Display for Circle
// Format: "Circle(r={})"

// TODO: Implement Shape for Circle
// area = PI * r^2, perimeter = 2 * PI * r

// TODO: Implement fmt::Display for Rectangle
// Format: "Rectangle({}x{})"

// TODO: Implement Shape for Rectangle
// area = width * height, perimeter = 2 * (width + height)

// TODO: Implement fmt::Display for Triangle
// Format: "Triangle({}, {}, {})"

// TODO: Implement Shape for Triangle
// area = use Heron's formula: sqrt(s*(s-a)*(s-b)*(s-c)) where s = perimeter/2
// perimeter = a + b + c

// TODO: Write a function `total_area` that takes a slice of trait objects
// `&[Box<dyn Shape>]` and returns the sum of all areas.

// TODO: Write a function `largest_shape` that takes a slice of trait objects
// `&[Box<dyn Shape>]` and returns a reference to the one with the largest area.
// Return `Option<&dyn Shape>`.

fn main() {
    let shapes: Vec<Box<dyn Shape>> = vec![
        Box::new(Circle { radius: 5.0 }),
        Box::new(Rectangle { width: 4.0, height: 6.0 }),
        Box::new(Triangle { a: 3.0, b: 4.0, c: 5.0 }),
    ];

    for shape in &shapes {
        println!("{}: area={:.2}, perimeter={:.2}", shape, shape.area(), shape.perimeter());
    }

    println!("Total area: {:.2}", total_area(&shapes));

    if let Some(biggest) = largest_shape(&shapes) {
        println!("Largest: {}", biggest);
    }
}
```

Expected output:
```
Circle(r=5): area=78.54, perimeter=31.42
Rectangle(4x6): area=24.00, perimeter=20.00
Triangle(3, 4, 5): area=6.00, perimeter=12.00
Total area: 108.54
Largest: Circle(r=5)
```

<details>
<summary>Solution</summary>

```rust
use std::f64::consts::PI;
use std::fmt;

trait Shape: fmt::Display {
    fn area(&self) -> f64;
    fn perimeter(&self) -> f64;
}

struct Circle { radius: f64 }
struct Rectangle { width: f64, height: f64 }
struct Triangle { a: f64, b: f64, c: f64 }

impl fmt::Display for Circle {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "Circle(r={})", self.radius)
    }
}

impl Shape for Circle {
    fn area(&self) -> f64 { PI * self.radius * self.radius }
    fn perimeter(&self) -> f64 { 2.0 * PI * self.radius }
}

impl fmt::Display for Rectangle {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "Rectangle({}x{})", self.width, self.height)
    }
}

impl Shape for Rectangle {
    fn area(&self) -> f64 { self.width * self.height }
    fn perimeter(&self) -> f64 { 2.0 * (self.width + self.height) }
}

impl fmt::Display for Triangle {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "Triangle({}, {}, {})", self.a, self.b, self.c)
    }
}

impl Shape for Triangle {
    fn area(&self) -> f64 {
        let s = self.perimeter() / 2.0;
        (s * (s - self.a) * (s - self.b) * (s - self.c)).sqrt()
    }
    fn perimeter(&self) -> f64 { self.a + self.b + self.c }
}

fn total_area(shapes: &[Box<dyn Shape>]) -> f64 {
    shapes.iter().map(|s| s.area()).sum()
}

fn largest_shape(shapes: &[Box<dyn Shape>]) -> Option<&dyn Shape> {
    shapes
        .iter()
        .max_by(|a, b| a.area().partial_cmp(&b.area()).unwrap())
        .map(|b| b.as_ref() as &dyn Shape)
}

fn main() {
    let shapes: Vec<Box<dyn Shape>> = vec![
        Box::new(Circle { radius: 5.0 }),
        Box::new(Rectangle { width: 4.0, height: 6.0 }),
        Box::new(Triangle { a: 3.0, b: 4.0, c: 5.0 }),
    ];

    for shape in &shapes {
        println!("{}: area={:.2}, perimeter={:.2}", shape, shape.area(), shape.perimeter());
    }

    println!("Total area: {:.2}", total_area(&shapes));

    if let Some(biggest) = largest_shape(&shapes) {
        println!("Largest: {}", biggest);
    }
}
```
</details>

### Exercise 2: Object Safety Exploration

Identify which traits are object-safe and fix the ones that are not.

```rust
use std::fmt;

// Trait A: Is this object-safe?
trait Drawable {
    fn draw(&self);
    fn resize(&mut self, factor: f64);
}

// Trait B: Is this object-safe? If not, why?
trait Cloneable {
    fn clone_it(&self) -> Self;
}

// Trait C: Is this object-safe? If not, why?
trait Serializer {
    fn serialize<W: std::io::Write>(&self, writer: &mut W);
}

// Trait D: Is this object-safe? If not, why?
trait Identifiable {
    fn id(&self) -> u64;
    fn create(id: u64) -> Self where Self: Sized;
}

// TODO: For each non-object-safe trait above, create a fixed version
// that CAN be used as `dyn Trait`.
//
// Hints:
// - For Cloneable: return Box<dyn CloneableFix> instead of Self
// - For Serializer: use &mut dyn Write instead of generic W
// - For Identifiable: add `where Self: Sized` to the problematic method
//   (this excludes it from the vtable but keeps the trait object-safe)

// TODO: Write a function `draw_all` that accepts `&[&dyn Drawable]`
// and draws every item.

// TODO: Create two structs (Sprite and Text) that implement Drawable,
// put them in a Vec<&dyn Drawable>, and call draw_all.

fn main() {
    // TODO: Build a Vec<&dyn Drawable> with mixed types and call draw_all
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::Write;

// Trait A: Object-safe — all methods have &self/&mut self, no generics, no Self return
trait Drawable {
    fn draw(&self);
    fn resize(&mut self, factor: f64);
}

// Trait B fixed: return Box<dyn CloneableFix> instead of Self
trait CloneableFix {
    fn clone_it(&self) -> Box<dyn CloneableFix>;
}

// Trait C fixed: use trait object instead of generic
trait SerializerFix {
    fn serialize(&self, writer: &mut dyn Write);
}

// Trait D fixed: `create` excluded from vtable via Sized bound
trait Identifiable {
    fn id(&self) -> u64;
    fn create(id: u64) -> Self where Self: Sized;
}

struct Sprite {
    name: String,
    scale: f64,
}

struct Text {
    content: String,
    font_size: f64,
}

impl Drawable for Sprite {
    fn draw(&self) {
        println!("Drawing sprite '{}' at scale {:.1}", self.name, self.scale);
    }
    fn resize(&mut self, factor: f64) {
        self.scale *= factor;
    }
}

impl Drawable for Text {
    fn draw(&self) {
        println!("Drawing text '{}' at size {:.1}", self.content, self.font_size);
    }
    fn resize(&mut self, factor: f64) {
        self.font_size *= factor;
    }
}

fn draw_all(items: &[&dyn Drawable]) {
    for item in items {
        item.draw();
    }
}

fn main() {
    let sprite = Sprite { name: "hero".into(), scale: 1.0 };
    let text = Text { content: "Score: 100".into(), font_size: 16.0 };

    let items: Vec<&dyn Drawable> = vec![&sprite, &text];
    draw_all(&items);
}
```
</details>

### Exercise 3: Plugin System with Downcasting

Build a plugin registry where plugins are stored as trait objects but can be downcast to their concrete types.

```rust
use std::any::Any;
use std::collections::HashMap;

trait Plugin: Any {
    fn name(&self) -> &str;
    fn execute(&self);
    fn as_any(&self) -> &dyn Any;
}

struct LoggerPlugin {
    level: String,
}

struct MetricsPlugin {
    endpoint: String,
    interval_secs: u64,
}

struct AuthPlugin {
    provider: String,
}

// TODO: Implement Plugin for LoggerPlugin
// - name() returns "logger"
// - execute() prints "Logging at level: {level}"
// - as_any() returns self

// TODO: Implement Plugin for MetricsPlugin
// - name() returns "metrics"
// - execute() prints "Sending metrics to {endpoint} every {interval_secs}s"
// - as_any() returns self

// TODO: Implement Plugin for AuthPlugin
// - name() returns "auth"
// - execute() prints "Authenticating via {provider}"
// - as_any() returns self

struct PluginRegistry {
    plugins: HashMap<String, Box<dyn Plugin>>,
}

impl PluginRegistry {
    fn new() -> Self {
        PluginRegistry { plugins: HashMap::new() }
    }

    // TODO: Implement `register` — inserts a Box<dyn Plugin> keyed by its name()

    // TODO: Implement `execute_all` — calls execute() on every plugin

    // TODO: Implement `get_plugin<T: 'static>` that returns Option<&T>
    // Look up by name, then downcast using as_any().downcast_ref::<T>()
}

fn main() {
    let mut registry = PluginRegistry::new();

    registry.register(Box::new(LoggerPlugin {
        level: "debug".into(),
    }));
    registry.register(Box::new(MetricsPlugin {
        endpoint: "http://metrics.local".into(),
        interval_secs: 30,
    }));
    registry.register(Box::new(AuthPlugin {
        provider: "OAuth2".into(),
    }));

    println!("=== Execute All ===");
    registry.execute_all();

    println!("\n=== Downcast ===");
    if let Some(metrics) = registry.get_plugin::<MetricsPlugin>("metrics") {
        println!("Metrics endpoint: {}", metrics.endpoint);
        println!("Metrics interval: {}s", metrics.interval_secs);
    }

    if let Some(auth) = registry.get_plugin::<AuthPlugin>("auth") {
        println!("Auth provider: {}", auth.provider);
    }

    // This should return None — wrong type for the name
    let wrong: Option<&LoggerPlugin> = registry.get_plugin::<LoggerPlugin>("metrics");
    println!("Wrong downcast: {:?}", wrong.is_none());
}
```

Expected output:
```
=== Execute All ===
Logging at level: debug
Sending metrics to http://metrics.local every 30s
Authenticating via OAuth2

=== Downcast ===
Metrics endpoint: http://metrics.local
Metrics interval: 30s
Auth provider: OAuth2
Wrong downcast: true
```

<details>
<summary>Solution</summary>

```rust
use std::any::Any;
use std::collections::HashMap;

trait Plugin: Any {
    fn name(&self) -> &str;
    fn execute(&self);
    fn as_any(&self) -> &dyn Any;
}

struct LoggerPlugin { level: String }
struct MetricsPlugin { endpoint: String, interval_secs: u64 }
struct AuthPlugin { provider: String }

impl Plugin for LoggerPlugin {
    fn name(&self) -> &str { "logger" }
    fn execute(&self) { println!("Logging at level: {}", self.level); }
    fn as_any(&self) -> &dyn Any { self }
}

impl Plugin for MetricsPlugin {
    fn name(&self) -> &str { "metrics" }
    fn execute(&self) {
        println!("Sending metrics to {} every {}s", self.endpoint, self.interval_secs);
    }
    fn as_any(&self) -> &dyn Any { self }
}

impl Plugin for AuthPlugin {
    fn name(&self) -> &str { "auth" }
    fn execute(&self) { println!("Authenticating via {}", self.provider); }
    fn as_any(&self) -> &dyn Any { self }
}

struct PluginRegistry {
    plugins: HashMap<String, Box<dyn Plugin>>,
}

impl PluginRegistry {
    fn new() -> Self {
        PluginRegistry { plugins: HashMap::new() }
    }

    fn register(&mut self, plugin: Box<dyn Plugin>) {
        let name = plugin.name().to_string();
        self.plugins.insert(name, plugin);
    }

    fn execute_all(&self) {
        for plugin in self.plugins.values() {
            plugin.execute();
        }
    }

    fn get_plugin<T: 'static>(&self, name: &str) -> Option<&T> {
        self.plugins
            .get(name)
            .and_then(|p| p.as_any().downcast_ref::<T>())
    }
}

fn main() {
    let mut registry = PluginRegistry::new();

    registry.register(Box::new(LoggerPlugin { level: "debug".into() }));
    registry.register(Box::new(MetricsPlugin {
        endpoint: "http://metrics.local".into(),
        interval_secs: 30,
    }));
    registry.register(Box::new(AuthPlugin { provider: "OAuth2".into() }));

    println!("=== Execute All ===");
    registry.execute_all();

    println!("\n=== Downcast ===");
    if let Some(metrics) = registry.get_plugin::<MetricsPlugin>("metrics") {
        println!("Metrics endpoint: {}", metrics.endpoint);
        println!("Metrics interval: {}s", metrics.interval_secs);
    }

    if let Some(auth) = registry.get_plugin::<AuthPlugin>("auth") {
        println!("Auth provider: {}", auth.provider);
    }

    let wrong: Option<&LoggerPlugin> = registry.get_plugin::<LoggerPlugin>("metrics");
    println!("Wrong downcast: {:?}", wrong.is_none());
}
```
</details>

### Exercise 4: `&dyn` vs `Box<dyn>` — Performance Comparison

Understand when borrowed trait objects avoid allocations.

```rust
trait Validator {
    fn validate(&self, input: &str) -> Result<(), String>;
    fn name(&self) -> &str;
}

struct LengthValidator {
    min: usize,
    max: usize,
}

struct RegexLikeValidator {
    must_contain: char,
}

struct CompositeValidator {
    // TODO: Store a Vec of BORROWED trait objects: Vec<&dyn Validator>
    // Why borrowed? Because CompositeValidator doesn't own the validators,
    // it just references them. This avoids heap allocation per validator.
    validators: (), // Replace this
}

// TODO: Implement Validator for LengthValidator
// - validate: check input.len() is in [min, max], return Err with message if not
// - name: "length"

// TODO: Implement Validator for RegexLikeValidator
// - validate: check input contains must_contain, return Err if not
// - name: "contains"

// TODO: Implement CompositeValidator with a lifetime parameter
// impl<'a> CompositeValidator<'a> { ... }
// - new(validators: Vec<&'a dyn Validator>) -> Self
// - validate_all(&self, input: &str) -> Vec<String>  (collect all errors)

fn main() {
    let length = LengthValidator { min: 5, max: 20 };
    let contains = RegexLikeValidator { must_contain: '@' };

    // No Box, no heap allocation — just references
    let composite = CompositeValidator::new(vec![&length, &contains]);

    let test_inputs = vec!["hi", "hello", "user@mail.com", "this-is-way-too-long-for-the-validator"];

    for input in test_inputs {
        let errors = composite.validate_all(input);
        if errors.is_empty() {
            println!("'{}': OK", input);
        } else {
            println!("'{}': ERRORS: {}", input, errors.join("; "));
        }
    }
}
```

Expected output:
```
'hi': ERRORS: length: must be between 5 and 20 chars; contains: must contain '@'
'hello': ERRORS: contains: must contain '@'
'user@mail.com': OK
'this-is-way-too-long-for-the-validator': ERRORS: length: must be between 5 and 20 chars
```

<details>
<summary>Solution</summary>

```rust
trait Validator {
    fn validate(&self, input: &str) -> Result<(), String>;
    fn name(&self) -> &str;
}

struct LengthValidator { min: usize, max: usize }
struct RegexLikeValidator { must_contain: char }

struct CompositeValidator<'a> {
    validators: Vec<&'a dyn Validator>,
}

impl Validator for LengthValidator {
    fn validate(&self, input: &str) -> Result<(), String> {
        if input.len() < self.min || input.len() > self.max {
            Err(format!("must be between {} and {} chars", self.min, self.max))
        } else {
            Ok(())
        }
    }
    fn name(&self) -> &str { "length" }
}

impl Validator for RegexLikeValidator {
    fn validate(&self, input: &str) -> Result<(), String> {
        if !input.contains(self.must_contain) {
            Err(format!("must contain '{}'", self.must_contain))
        } else {
            Ok(())
        }
    }
    fn name(&self) -> &str { "contains" }
}

impl<'a> CompositeValidator<'a> {
    fn new(validators: Vec<&'a dyn Validator>) -> Self {
        CompositeValidator { validators }
    }

    fn validate_all(&self, input: &str) -> Vec<String> {
        self.validators
            .iter()
            .filter_map(|v| {
                v.validate(input)
                    .err()
                    .map(|e| format!("{}: {}", v.name(), e))
            })
            .collect()
    }
}

fn main() {
    let length = LengthValidator { min: 5, max: 20 };
    let contains = RegexLikeValidator { must_contain: '@' };

    let composite = CompositeValidator::new(vec![&length, &contains]);

    let test_inputs = vec!["hi", "hello", "user@mail.com", "this-is-way-too-long-for-the-validator"];

    for input in test_inputs {
        let errors = composite.validate_all(input);
        if errors.is_empty() {
            println!("'{}': OK", input);
        } else {
            println!("'{}': ERRORS: {}", input, errors.join("; "));
        }
    }
}
```
</details>

### Exercise 5: Trait Object with `Fn` Trait — Callback Registry

Store closures as trait objects to build a flexible event system.

```rust
use std::collections::HashMap;

struct EventBus {
    // TODO: Store handlers as Vec<Box<dyn Fn(&str)>> per event name
    // Key: event name (String), Value: list of handlers
    handlers: HashMap<String, Vec<Box<dyn Fn(&str)>>>,
}

impl EventBus {
    fn new() -> Self {
        EventBus { handlers: HashMap::new() }
    }

    // TODO: Implement `on` — register a handler for an event name
    // The handler is any closure that takes &str and returns ()
    // fn on(&mut self, event: &str, handler: impl Fn(&str) + 'static)

    // TODO: Implement `emit` — call all handlers registered for the event
    // Pass the payload string to each handler
    // fn emit(&self, event: &str, payload: &str)
}

fn main() {
    let mut bus = EventBus::new();

    bus.on("user:login", |payload| {
        println!("[audit] User logged in: {}", payload);
    });

    bus.on("user:login", |payload| {
        println!("[metrics] Login event for: {}", payload);
    });

    bus.on("order:created", |payload| {
        println!("[email] Order confirmation: {}", payload);
    });

    bus.on("order:created", |payload| {
        println!("[inventory] Reserve stock for: {}", payload);
    });

    println!("--- Emitting user:login ---");
    bus.emit("user:login", "alice@example.com");

    println!("\n--- Emitting order:created ---");
    bus.emit("order:created", "order-42");

    println!("\n--- Emitting unknown event ---");
    bus.emit("system:shutdown", "now");
}
```

Expected output:
```
--- Emitting user:login ---
[audit] User logged in: alice@example.com
[metrics] Login event for: alice@example.com

--- Emitting order:created ---
[email] Order confirmation: order-42
[inventory] Reserve stock for: order-42

--- Emitting unknown event ---
```

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

struct EventBus {
    handlers: HashMap<String, Vec<Box<dyn Fn(&str)>>>,
}

impl EventBus {
    fn new() -> Self {
        EventBus { handlers: HashMap::new() }
    }

    fn on(&mut self, event: &str, handler: impl Fn(&str) + 'static) {
        self.handlers
            .entry(event.to_string())
            .or_insert_with(Vec::new)
            .push(Box::new(handler));
    }

    fn emit(&self, event: &str, payload: &str) {
        if let Some(handlers) = self.handlers.get(event) {
            for handler in handlers {
                handler(payload);
            }
        }
    }
}

fn main() {
    let mut bus = EventBus::new();

    bus.on("user:login", |payload| {
        println!("[audit] User logged in: {}", payload);
    });
    bus.on("user:login", |payload| {
        println!("[metrics] Login event for: {}", payload);
    });
    bus.on("order:created", |payload| {
        println!("[email] Order confirmation: {}", payload);
    });
    bus.on("order:created", |payload| {
        println!("[inventory] Reserve stock for: {}", payload);
    });

    println!("--- Emitting user:login ---");
    bus.emit("user:login", "alice@example.com");

    println!("\n--- Emitting order:created ---");
    bus.emit("order:created", "order-42");

    println!("\n--- Emitting unknown event ---");
    bus.emit("system:shutdown", "now");
}
```
</details>

## Common Mistakes

### Mistake 1: Using a Non-Object-Safe Trait as `dyn`

```rust
trait MyClone {
    fn clone_self(&self) -> Self;
}

// Error: the trait `MyClone` cannot be made into an object
let _: Box<dyn MyClone>;
```

**Fix**: Return `Box<dyn MyClone>` instead of `Self`, or add `where Self: Sized` to exclude the method from the vtable.

### Mistake 2: Forgetting `'static` on Boxed Closures

```rust
fn register(handler: Box<dyn Fn()>) { /* ... */ }

let name = String::from("alice");
// This closure borrows `name`, but Box<dyn Fn()> requires 'static
register(Box::new(|| println!("{}", name))); // Error!
```

**Fix**: Move the captured variable into the closure with `move`:

```rust
register(Box::new(move || println!("{}", name)));
```

### Mistake 3: Expecting Trait Objects to Know Their Concrete Type

```rust
fn print_type(shape: &dyn Shape) {
    // Cannot call concrete-type methods — only trait methods are available
    // shape.radius; // Error! No field access through trait objects.
}
```

**Fix**: Use `as_any()` + `downcast_ref::<ConcreteType>()` if you truly need the concrete type.

## Verification

```bash
# For each exercise, paste the code into main.rs and run:
cargo run
```

Test your understanding:
1. Try creating a `Vec<dyn Shape>` (without `Box` or `&`) — why does it fail?
2. Try adding a method `fn new() -> Self` to `Shape` — what error do you get?
3. Measure the size of `Box<dyn Shape>` with `std::mem::size_of` — confirm it is 16 bytes (two pointers).

## What You Learned

- Trait objects (`dyn Trait`) enable runtime polymorphism through vtable-based dynamic dispatch.
- `&dyn Trait` borrows without allocation; `Box<dyn Trait>` owns on the heap.
- Object safety rules restrict which traits can be used as trait objects: no `Self` returns, no generic methods.
- `Any` enables downcasting when you need to recover the concrete type.
- Prefer generics for performance-critical code; use trait objects for heterogeneous collections and plugin architectures.
- Closures can be stored as `Box<dyn Fn(...)>` to build flexible callback systems.

## What's Next

Exercise 27 covers interior mutability with `Cell`, `RefCell`, and `Rc<RefCell<T>>` — patterns that combine naturally with trait objects for shared mutable state.

## Resources

- [The Rust Book — Trait Objects](https://doc.rust-lang.org/book/ch18-02-trait-objects.html)
- [Rust Reference — Object Safety](https://doc.rust-lang.org/reference/items/traits.html#object-safety)
- [std::any::Any](https://doc.rust-lang.org/std/any/trait.Any.html)
- [Rust Performance Book — Dynamic Dispatch](https://nnethercote.github.io/perf-book/type-sizes.html)
