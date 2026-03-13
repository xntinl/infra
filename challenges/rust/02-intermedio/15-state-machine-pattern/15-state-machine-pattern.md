# 15. State Machine Pattern

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-basico exercises (structs, enums, ownership)
- Completed: 01-traits, 02-generics, 13-newtype-pattern, 14-builder-pattern
- Familiar with `PhantomData`, generic type parameters, and consuming methods

## Learning Objectives

- Apply the typestate pattern to model state machines where invalid transitions are compile-time errors
- Implement state-specific methods using generic type parameters and `PhantomData`
- Analyze the trade-offs between typestate (compile-time) and enum-based (runtime) state machines
- Evaluate when each approach is appropriate based on the problem domain
- Design an order processing workflow that enforces transition rules at the type level

## Concepts

### What Is a State Machine?

A state machine is any system that can be in one of a finite number of states, with defined transitions between them. You encounter them everywhere: an HTTP connection is `Idle -> Connecting -> Connected -> Closed`. A document is `Draft -> Review -> Published`. An order is `Created -> Paid -> Shipped -> Delivered`.

The key property is that not all transitions are valid. You cannot ship an order that has not been paid for. You cannot publish a document that has not been reviewed. The question is: when do you catch invalid transitions?

### The Enum Approach (Runtime)

The most straightforward encoding uses an enum for state:

```rust
enum OrderState {
    Draft,
    Pending,
    Shipped,
    Delivered,
    Cancelled,
}

struct Order {
    id: u64,
    state: OrderState,
}

impl Order {
    fn ship(&mut self) -> Result<(), String> {
        match self.state {
            OrderState::Pending => {
                self.state = OrderState::Shipped;
                Ok(())
            }
            _ => Err(format!("cannot ship order in {:?} state", self.state)),
        }
    }
}
```

This is simple and flexible. It handles any number of states, works with dynamic dispatch, and serializes easily. But invalid transitions are checked at runtime. If you forget a check, the bug ships to production.

### The Typestate Approach (Compile-Time)

The typestate pattern encodes the current state as a type parameter. Each state is a separate type, and transitions are methods that consume the value in one state and return it in another:

```rust
use std::marker::PhantomData;

struct Draft;
struct Pending;
struct Shipped;
struct Delivered;

struct Order<State> {
    id: u64,
    _state: PhantomData<State>,
}

impl Order<Draft> {
    fn submit(self) -> Order<Pending> {
        println!("Order {} submitted", self.id);
        Order { id: self.id, _state: PhantomData }
    }
}

impl Order<Pending> {
    fn ship(self) -> Order<Shipped> {
        println!("Order {} shipped", self.id);
        Order { id: self.id, _state: PhantomData }
    }
}

impl Order<Shipped> {
    fn deliver(self) -> Order<Delivered> {
        println!("Order {} delivered", self.id);
        Order { id: self.id, _state: PhantomData }
    }
}
```

Now this code does not compile:

```rust
let order = Order::<Draft>::new(1);
order.ship(); // ERROR: ship() is only defined for Order<Pending>
```

The compiler prevents the invalid transition. No runtime check needed.

### Why `PhantomData`?

`PhantomData<State>` tells the compiler "this struct is parameterized by `State` even though it does not contain a value of that type." Without it, the compiler rejects unused type parameters:

```rust
// This won't compile:
struct Order<State> {
    id: u64,
    // ERROR: parameter `State` is never used
}

// This works:
struct Order<State> {
    id: u64,
    _state: PhantomData<State>,
}
```

`PhantomData` is zero-sized -- it adds no bytes to the struct. The type parameter exists only at compile time.

### Shared Methods Across States

Methods that apply to all states are implemented generically:

```rust
impl<State> Order<State> {
    fn id(&self) -> u64 {
        self.id
    }
}
```

Methods specific to one state are implemented on that concrete type:

```rust
impl Order<Shipped> {
    fn tracking_number(&self) -> &str {
        "TRK-12345"
    }
}
```

Calling `tracking_number()` on an `Order<Draft>` is a compile error. The method literally does not exist for that type.

### Adding Data Per State

Real state machines carry different data in different states. A shipped order has a tracking number; a draft does not. You can use state types that hold data:

```rust
struct Draft;
struct Pending { submitted_at: String }
struct Shipped { tracking: String, shipped_at: String }
struct Delivered { delivered_at: String }

struct Order<S> {
    id: u64,
    items: Vec<String>,
    state: S,  // now it actually holds the state data
}

impl Order<Pending> {
    fn ship(self, tracking: String) -> Order<Shipped> {
        Order {
            id: self.id,
            items: self.items,
            state: Shipped {
                tracking,
                shipped_at: "2026-03-12".to_string(),
            },
        }
    }
}
```

When the state types hold data, you no longer need `PhantomData` -- the `state` field itself carries the type parameter.

### Enum vs Typestate: When to Use Which

| Factor | Enum (runtime) | Typestate (compile-time) |
|---|---|---|
| Number of states | Any | Best for < 10 |
| Transition validation | Runtime (`Result` or panic) | Compile-time (method not available) |
| State stored in collections | Easy (`Vec<Order>`) | Hard (`Vec<Order<??>>` -- different types) |
| Serialization | Straightforward | Requires conversion to/from enum |
| Dynamic transitions | Natural (read state from DB) | Unnatural (must match and convert) |
| API safety | Caller can forget checks | Compiler prevents invalid calls |
| Code volume | Less | More (one impl block per state) |

**Rule of thumb**: Use typestate for protocol-like workflows with a small, fixed set of states where safety matters (payment processing, authentication flows, protocol implementations). Use enums for UI state, game logic, or anything with many states or dynamic transitions.

### Combining Both Approaches

In practice, you can use typestate internally and expose an enum at API boundaries:

```rust
// Internal: typestate for safety
fn process_order(order: Order<Pending>) -> Order<Shipped> { /* ... */ }

// External: enum for serialization/storage
enum AnyOrder {
    Draft(Order<Draft>),
    Pending(Order<Pending>),
    Shipped(Order<Shipped>),
    Delivered(Order<Delivered>),
}
```

## Exercises

### Exercise 1: Simple Document Workflow

Model a document that flows through: `Draft -> UnderReview -> Approved -> Published`.

```rust
use std::marker::PhantomData;

// State types
struct Draft;
struct UnderReview;
struct Approved;
struct Published;

struct Document<State> {
    title: String,
    content: String,
    _state: PhantomData<State>,
}

// TODO: Implement Document<Draft> with:
//   fn new(title: &str, content: &str) -> Document<Draft>
//   fn edit(&mut self, new_content: &str)  -- only drafts can be edited
//   fn submit_for_review(self) -> Document<UnderReview>

// TODO: Implement Document<UnderReview> with:
//   fn approve(self) -> Document<Approved>
//   fn reject(self) -> Document<Draft>  -- goes back to draft

// TODO: Implement Document<Approved> with:
//   fn publish(self) -> Document<Published>

// TODO: Implement Document<Published> with:
//   fn url(&self) -> String  -- returns a fake URL based on title

// TODO: Implement for ALL states (generic):
//   fn title(&self) -> &str
//   fn content(&self) -> &str

fn main() {
    // Happy path:
    let doc = Document::<Draft>::new("Rust Guide", "Chapter 1...");
    let doc = doc.submit_for_review();
    let doc = doc.approve();
    let doc = doc.publish();
    println!("Published at: {}", doc.url());

    // Rejection path:
    let mut doc = Document::<Draft>::new("RFC", "Proposal...");
    doc.edit("Revised proposal...");
    let doc = doc.submit_for_review();
    let mut doc = doc.reject(); // back to draft
    doc.edit("Better proposal...");
    let doc = doc.submit_for_review();
    let doc = doc.approve();
    let doc = doc.publish();
    println!("Finally published: {}", doc.title());

    // These should NOT compile -- uncomment to verify:
    // let doc = Document::<Draft>::new("Test", "content");
    // doc.approve(); // ERROR: approve() not on Draft
    // doc.publish(); // ERROR: publish() not on Draft

    // let doc = doc.submit_for_review();
    // doc.edit("change"); // ERROR: edit() not on UnderReview
}
```

### Exercise 2: Stateful Data per State

Extend the document workflow with state-specific data.

```rust
// States now carry data relevant to that stage:
struct Draft {
    created_at: String,
}

struct UnderReview {
    submitted_at: String,
    reviewer: String,
}

struct Approved {
    approved_at: String,
    approved_by: String,
}

struct Published {
    published_at: String,
    url: String,
}

struct Document<S> {
    title: String,
    content: String,
    version: u32,
    state: S,  // no PhantomData needed; state field carries the type
}

// TODO: Implement Document<Draft> with:
//   fn new(title: &str, content: &str) -> Document<Draft>
//     (set created_at to "2026-03-12", version to 1)
//   fn edit(&mut self, content: &str)
//     (updates content and increments version)
//   fn submit(self, reviewer: &str) -> Document<UnderReview>

// TODO: Implement Document<UnderReview> with:
//   fn approve(self, approver: &str) -> Document<Approved>
//   fn reject(self, reason: &str) -> Document<Draft>
//     (print the reason, reset to draft with incremented version)

// TODO: Implement Document<Approved> with:
//   fn publish(self) -> Document<Published>
//     (generate a URL from the title, like "/articles/rust-guide")

// TODO: Implement generic methods for all states:
//   fn title(&self) -> &str
//   fn version(&self) -> u32

// TODO: Implement Display for Document<Published> showing title + URL.

fn main() {
    let doc = Document::<Draft>::new("Rust Guide", "Introduction to Rust");
    println!("Created: {}", doc.state.created_at);

    let doc = doc.submit("Alice");
    println!("Reviewer: {}", doc.state.reviewer);

    let doc = doc.approve("Bob");
    println!("Approved by: {}", doc.state.approved_by);

    let doc = doc.publish();
    println!("URL: {}", doc.state.url);
    println!("Version: {}", doc.version());
}
```

### Exercise 3: Order Processing Pipeline

Build a full order processing state machine:

`Created -> Paid -> Processing -> Shipped -> Delivered`

With a `Cancelled` state reachable from `Created` and `Paid`.

```rust
use std::fmt;

// State types with relevant data:
struct Created {
    created_at: String,
}

struct Paid {
    paid_at: String,
    amount_cents: u64,
    payment_id: String,
}

struct Processing {
    warehouse: String,
    started_at: String,
}

struct Shipped {
    tracking_number: String,
    carrier: String,
    shipped_at: String,
}

struct Delivered {
    delivered_at: String,
    signed_by: String,
}

struct Cancelled {
    reason: String,
    cancelled_at: String,
}

struct Order<S> {
    id: u64,
    customer: String,
    items: Vec<String>,
    state: S,
}

// TODO: Implement Order<Created> with:
//   fn new(id: u64, customer: &str, items: Vec<String>) -> Order<Created>
//   fn pay(self, amount_cents: u64, payment_id: &str) -> Order<Paid>
//   fn cancel(self, reason: &str) -> Order<Cancelled>

// TODO: Implement Order<Paid> with:
//   fn start_processing(self, warehouse: &str) -> Order<Processing>
//   fn cancel(self, reason: &str) -> Order<Cancelled>
//     (in real life, this would also trigger a refund)

// TODO: Implement Order<Processing> with:
//   fn ship(self, tracking: &str, carrier: &str) -> Order<Shipped>

// TODO: Implement Order<Shipped> with:
//   fn deliver(self, signed_by: &str) -> Order<Delivered>

// TODO: Implement Display for Order<S> (generic, show id and customer)

// TODO: Implement state-specific Display messages:
//   Order<Shipped> should also show tracking number
//   Order<Cancelled> should show the reason

// TODO: Implement generic methods for all states:
//   fn id(&self) -> u64
//   fn customer(&self) -> &str
//   fn items(&self) -> &[String]

fn main() {
    // Happy path:
    let order = Order::<Created>::new(
        1001,
        "Alice",
        vec!["Rust Book".into(), "Keyboard".into()],
    );
    println!("Created: {order}");

    let order = order.pay(4999, "PAY-abc123");
    println!("Paid: {} cents", order.state.amount_cents);

    let order = order.start_processing("Warehouse-East");
    println!("Processing at: {}", order.state.warehouse);

    let order = order.ship("TRK-789", "FastShip");
    println!("Shipped via {}: {}", order.state.carrier, order.state.tracking_number);

    let order = order.deliver("Alice Smith");
    println!("Delivered, signed by: {}", order.state.signed_by);

    // Cancellation path:
    let order2 = Order::<Created>::new(
        1002,
        "Bob",
        vec!["Mouse".into()],
    );
    let cancelled = order2.cancel("Changed my mind");
    println!("Cancelled: {}", cancelled.state.reason);

    // These should NOT compile -- uncomment to verify:
    // let order = Order::<Created>::new(1003, "Carol", vec![]);
    // order.ship("TRK", "Carrier"); // ERROR: cannot ship a Created order
    // order.deliver("Someone");     // ERROR: cannot deliver a Created order

    // After payment, cannot cancel via the Created cancel method:
    // let paid_order = order.pay(100, "PAY-xyz");
    // This uses Order<Paid>::cancel, which is a different method (triggers refund):
    // let refunded = paid_order.cancel("Found it cheaper");
}
```

### Exercise 4: Enum-Based State Machine (for Comparison)

Implement the same order workflow using an enum instead of typestate.

```rust
use std::fmt;

#[derive(Debug, Clone)]
enum OrderState {
    Created,
    Paid { amount_cents: u64, payment_id: String },
    Processing { warehouse: String },
    Shipped { tracking: String, carrier: String },
    Delivered { signed_by: String },
    Cancelled { reason: String },
}

#[derive(Debug)]
struct Order {
    id: u64,
    customer: String,
    items: Vec<String>,
    state: OrderState,
}

#[derive(Debug)]
enum OrderError {
    InvalidTransition { from: String, action: String },
}

impl fmt::Display for OrderError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            OrderError::InvalidTransition { from, action } => {
                write!(f, "cannot {action} from {from} state")
            }
        }
    }
}

impl Order {
    fn new(id: u64, customer: &str, items: Vec<String>) -> Self {
        Order {
            id,
            customer: customer.to_string(),
            items,
            state: OrderState::Created,
        }
    }

    fn state_name(&self) -> &str {
        match &self.state {
            OrderState::Created => "created",
            OrderState::Paid { .. } => "paid",
            OrderState::Processing { .. } => "processing",
            OrderState::Shipped { .. } => "shipped",
            OrderState::Delivered { .. } => "delivered",
            OrderState::Cancelled { .. } => "cancelled",
        }
    }

    // TODO: Implement pay(&mut self, amount: u64, payment_id: &str) -> Result<(), OrderError>
    // Only valid from Created state.

    // TODO: Implement start_processing(&mut self, warehouse: &str) -> Result<(), OrderError>
    // Only valid from Paid state.

    // TODO: Implement ship(&mut self, tracking: &str, carrier: &str) -> Result<(), OrderError>
    // Only valid from Processing state.

    // TODO: Implement deliver(&mut self, signed_by: &str) -> Result<(), OrderError>
    // Only valid from Shipped state.

    // TODO: Implement cancel(&mut self, reason: &str) -> Result<(), OrderError>
    // Only valid from Created or Paid states.
}

fn main() {
    let mut order = Order::new(1001, "Alice", vec!["Book".into()]);
    println!("State: {}", order.state_name());

    // Valid transitions:
    order.pay(2999, "PAY-123").unwrap();
    order.start_processing("Warehouse-A").unwrap();
    order.ship("TRK-456", "Express").unwrap();
    order.deliver("Alice").unwrap();
    println!("Final state: {}", order.state_name());

    // Invalid transition:
    let mut order2 = Order::new(1002, "Bob", vec!["Mouse".into()]);
    let result = order2.ship("TRK", "Carrier"); // should fail
    println!("Invalid: {}", result.unwrap_err());

    // The enum approach lets you store mixed orders:
    let orders: Vec<Order> = vec![
        Order::new(1, "A", vec![]),
        Order::new(2, "B", vec![]),
    ];
    // This is impossible with typestate (different types can't go in one Vec).
    println!("Mixed orders: {}", orders.len());
}
```

After completing both Exercise 3 and 4, answer these questions:

1. Which approach caught the "ship a created order" bug? At compile time or runtime?
2. Which approach lets you store orders in a `Vec` regardless of state?
3. Which approach would you use for a REST API that loads orders from a database?
4. Which approach would you use for a payment SDK where safety is critical?

### Exercise 5: Connection Protocol

Model a TCP-like connection protocol: `Closed -> Connecting -> Connected -> Closed`.

```rust
use std::marker::PhantomData;
use std::fmt;

struct Closed;
struct Connecting;
struct Connected;

struct Connection<S> {
    address: String,
    _state: PhantomData<S>,
}

// TODO: Implement Connection<Closed> with:
//   fn new(address: &str) -> Connection<Closed>
//   fn connect(self) -> Connection<Connecting>

// TODO: Implement Connection<Connecting> with:
//   fn on_connected(self) -> Connection<Connected>
//     (simulates successful connection)
//   fn on_failed(self) -> Connection<Closed>
//     (simulates connection failure, returns to Closed)

// TODO: Implement Connection<Connected> with:
//   fn send(&self, data: &str)
//     (prints "Sending to {address}: {data}")
//   fn receive(&self) -> String
//     (returns a dummy response)
//   fn disconnect(self) -> Connection<Closed>

// TODO: Implement Display for Connection<S> showing the address and state name.
// Hint: Use a trait to get the state name.
trait StateName {
    fn state_name() -> &'static str;
}

impl StateName for Closed {
    fn state_name() -> &'static str { "closed" }
}
// TODO: Implement for Connecting and Connected.

impl<S: StateName> fmt::Display for Connection<S> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "Connection({}, {})", self.address, S::state_name())
    }
}

fn main() {
    // Successful connection:
    let conn = Connection::<Closed>::new("192.168.1.1:8080");
    println!("{conn}"); // Connection(192.168.1.1:8080, closed)

    let conn = conn.connect();
    println!("{conn}"); // Connection(192.168.1.1:8080, connecting)

    let conn = conn.on_connected();
    println!("{conn}"); // Connection(192.168.1.1:8080, connected)

    conn.send("Hello, server!");
    let response = conn.receive();
    println!("Received: {response}");

    let conn = conn.disconnect();
    println!("{conn}"); // Connection(192.168.1.1:8080, closed)

    // Reconnection after failure:
    let conn = conn.connect();
    let conn = conn.on_failed(); // back to closed
    println!("After failure: {conn}");
    let conn = conn.connect(); // can try again
    let conn = conn.on_connected();
    conn.send("Retry succeeded!");
    let _conn = conn.disconnect();

    // These should NOT compile -- uncomment to verify:
    // let closed = Connection::<Closed>::new("example.com:80");
    // closed.send("data"); // ERROR: send() not on Closed
    // closed.disconnect();  // ERROR: disconnect() not on Closed

    // let connecting = closed.connect();
    // connecting.send("data"); // ERROR: send() not on Connecting
}
```

## Try It Yourself

1. **Authentication flow**: Model `Anonymous -> Authenticating -> Authenticated -> Expired`. Only `Authenticated` can access resources. `Expired` can refresh to become `Authenticated` again. Include user data only in the `Authenticated` state.

2. **File handle**: Model `Closed -> OpenRead -> Closed` and `Closed -> OpenWrite -> Closed`. A file open for reading has a `read()` method. A file open for writing has `write()` and `flush()`. You cannot read a write-handle or write to a read-handle.

3. **Hybrid approach**: Take the order processing from Exercise 3 and add a `fn into_enum(self) -> OrderEnum` method on each typestate variant. Use this to store heterogeneous orders in a `Vec<OrderEnum>` after processing.

4. **State machine diagram**: Draw (on paper or in ASCII) the state diagram for the Connection protocol in Exercise 5. Label each edge with the method that triggers the transition. Verify that the diagram matches the code -- every arrow should have exactly one method, and no arrows should be missing.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Forgetting to consume `self` in transitions | Old state still accessible after transition | Use `self` (not `&self` or `&mut self`) for state transitions |
| Implementing methods on generic `Order<S>` when they should be state-specific | Method available in wrong state | Implement on concrete `Order<Shipped>`, not `Order<S>` |
| Using `PhantomData` when state types carry data | Unnecessary complexity | If your state struct has fields, store it directly in the state field |
| Trying to store typestate values in a collection | "expected X, found Y" type errors | Use an enum wrapper or convert to an enum before storing |
| Not implementing `Clone`/`Copy` for marker types | Cannot derive common traits on the parent struct | Derive `Clone`, `Copy` on simple marker types |
| Infinite state machine growth | One impl block per state per method | Consider enum approach if you have more than 6-8 states |

## Verification

- Exercise 1: Valid transitions compile. Invalid transitions (skip states, call wrong methods) produce compile errors.
- Exercise 2: State-specific data is accessible. Version increments on edits and rejections.
- Exercise 3: Full order pipeline works. Cancellation from Created and Paid states works. Direct shipping from Created does not compile.
- Exercise 4: Same workflow, but errors are caught at runtime with `Result`. Mixed orders stored in a `Vec`.
- Exercise 5: Connection lifecycle works. Send/receive only on Connected. Failed connections return to Closed.

## Summary

State machines are everywhere in software. Rust gives you two ways to encode them. The enum approach is simple, flexible, and handles dynamic dispatch and serialization well, but it checks transitions at runtime. The typestate approach uses the type system to make invalid transitions uncompilable, at the cost of more boilerplate and the inability to store heterogeneous states in collections. Neither approach is universally better. Use typestate when the cost of an invalid transition is high (payments, protocols, security) and the number of states is manageable. Use enums when you need flexibility, persistence, or many states. And when the situation calls for it, combine both: typestate internally for safety, enum at the boundaries for interoperability.

## What's Next

This is the final exercise in the Intermedio series. You have covered traits, generics, lifetimes, closures, iterators, smart pointers, error handling, testing, modules, Cargo, type conversions, operator overloading, and three design patterns (newtype, builder, state machine). The Avanzado series will build on all of this with async/await, unsafe Rust, macros, and concurrency.

## Resources

- [The Typestate Pattern in Rust](https://cliffle.com/blog/rust-typestate/)
- [Rust Design Patterns: State](https://rust-unofficial.github.io/patterns/patterns/behavioural/state.html)
- [Typestates in Rust (Hoverbear)](https://hoverbear.org/blog/rust-state-machine-pattern/)
- [PhantomData documentation](https://doc.rust-lang.org/std/marker/struct.PhantomData.html)
- [Rustc Dev Guide: Type System](https://rustc-dev-guide.rust-lang.org/type-system.html)
