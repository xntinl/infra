# 17. Compile-Time Guarantees

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-16 (traits, generics, error handling, lifetimes)
- Understanding of generic type parameters and trait bounds
- Familiarity with zero-sized types (e.g., `PhantomData`)
- Comfort reading complex where clauses and associated types

## Learning Objectives

- Use phantom types to encode validation state in the type system
- Implement branded types that prevent unit confusion at compile time
- Design session types (protocol state machines) where invalid transitions fail to compile
- Apply sealed traits to restrict implementations to your crate
- Use marker traits to classify types without adding behavior
- Leverage `compile_error!` and const assertions for conditional compilation guards
- Evaluate when compile-time encoding improves vs overcomplicates a design

## Concepts

### The Core Idea

Runtime checks fail at runtime. Tests catch some failures before production. The type system catches failures before the code compiles. Every invariant you encode in types is an invariant you never need to test, log, or debug in production.

The cost: more complex type signatures. The goal is to find the sweet spot where the type-level complexity pays for itself in eliminated bug classes.

### Phantom Types: State Without Data

`PhantomData<T>` lets a struct carry a type parameter without storing any value of that type. The parameter exists only for the compiler:

```rust
use std::marker::PhantomData;

struct Unvalidated;
struct Validated;

struct Email<State = Unvalidated> {
    address: String,
    _state: PhantomData<State>,
}

impl Email<Unvalidated> {
    fn new(address: String) -> Self {
        Email { address, _state: PhantomData }
    }

    fn validate(self) -> Result<Email<Validated>, String> {
        if self.address.contains('@') && self.address.contains('.') {
            Ok(Email {
                address: self.address,
                _state: PhantomData,
            })
        } else {
            Err(format!("invalid email: {}", self.address))
        }
    }
}

impl Email<Validated> {
    fn send(&self, body: &str) {
        println!("Sending to {}: {body}", self.address);
    }
}
```

`Email<Unvalidated>` and `Email<Validated>` are different types. You cannot call `.send()` on an unvalidated email -- the method does not exist on that type. No runtime check needed.

```rust
let raw = Email::new("user@example.com".into());
// raw.send("hello");  // Compile error: method not found
let valid = raw.validate().unwrap();
valid.send("hello");   // Works
```

### Branded Types: Unit Safety

A branded type wraps a primitive but uses a phantom parameter to prevent mixing values of different units:

```rust
use std::marker::PhantomData;
use std::ops::Add;

struct Meters;
struct Feet;
struct Seconds;

#[derive(Debug, Clone, Copy)]
struct Quantity<Unit> {
    value: f64,
    _unit: PhantomData<Unit>,
}

impl<U> Quantity<U> {
    fn new(value: f64) -> Self {
        Quantity { value, _unit: PhantomData }
    }

    fn value(self) -> f64 {
        self.value
    }
}

// Addition only works for same units
impl<U> Add for Quantity<U> {
    type Output = Self;
    fn add(self, rhs: Self) -> Self {
        Quantity::new(self.value + rhs.value)
    }
}

// Explicit conversion between units
impl Quantity<Feet> {
    fn to_meters(self) -> Quantity<Meters> {
        Quantity::new(self.value * 0.3048)
    }
}

impl Quantity<Meters> {
    fn to_feet(self) -> Quantity<Feet> {
        Quantity::new(self.value / 0.3048)
    }
}
```

This is the pattern that would have prevented the Mars Climate Orbiter loss. You cannot add `Quantity<Meters>` and `Quantity<Feet>` -- the compiler rejects it:

```rust
let a = Quantity::<Meters>::new(100.0);
let b = Quantity::<Feet>::new(328.0);
// let c = a + b;  // Compile error: mismatched types
let c = a + b.to_meters();  // Works
```

### Typestate Pattern: Protocol State Machines

Typestates model multi-step protocols where certain operations are only valid in certain states. The classic example is an HTTP request builder:

```rust
use std::marker::PhantomData;

// States
struct NeedsMethod;
struct NeedsUrl;
struct NeedsBody;
struct Ready;

struct RequestBuilder<State> {
    method: Option<String>,
    url: Option<String>,
    body: Option<String>,
    headers: Vec<(String, String)>,
    _state: PhantomData<State>,
}

impl RequestBuilder<NeedsMethod> {
    fn new() -> Self {
        RequestBuilder {
            method: None,
            url: None,
            body: None,
            headers: Vec::new(),
            _state: PhantomData,
        }
    }

    fn method(self, method: &str) -> RequestBuilder<NeedsUrl> {
        RequestBuilder {
            method: Some(method.to_string()),
            url: self.url,
            body: self.body,
            headers: self.headers,
            _state: PhantomData,
        }
    }
}

impl RequestBuilder<NeedsUrl> {
    fn url(self, url: &str) -> RequestBuilder<Ready> {
        RequestBuilder {
            method: self.method,
            url: Some(url.to_string()),
            body: self.body,
            headers: self.headers,
            _state: PhantomData,
        }
    }
}

impl RequestBuilder<Ready> {
    // Headers can be added in the Ready state
    fn header(mut self, key: &str, value: &str) -> Self {
        self.headers.push((key.to_string(), value.to_string()));
        self
    }

    fn body(self, body: &str) -> Self {
        RequestBuilder {
            body: Some(body.to_string()),
            ..self
        }
    }

    fn send(self) -> String {
        format!(
            "{} {} (headers: {}, body: {:?})",
            self.method.unwrap(),
            self.url.unwrap(),
            self.headers.len(),
            self.body
        )
    }
}
```

The only valid sequence is `new() -> method() -> url() -> [header()/body()] -> send()`. Attempting to call `.send()` before `.url()` produces a compile error, not a runtime panic.

### Session Types: Bidirectional Protocols

Session types extend typestates to model bidirectional communication protocols. Each state encodes what both parties can do:

```rust
use std::marker::PhantomData;

// Protocol: Client sends Request, Server sends Response, then Done
struct ClientSendsRequest;
struct ServerSendsResponse;
struct Done;

struct Channel<State> {
    // In production, this would wrap a socket or channel
    buffer: Vec<String>,
    _state: PhantomData<State>,
}

impl Channel<ClientSendsRequest> {
    fn new() -> Self {
        Channel { buffer: Vec::new(), _state: PhantomData }
    }

    fn send_request(mut self, msg: &str) -> Channel<ServerSendsResponse> {
        self.buffer.push(format!("REQ: {msg}"));
        Channel { buffer: self.buffer, _state: PhantomData }
    }
}

impl Channel<ServerSendsResponse> {
    fn send_response(mut self, msg: &str) -> Channel<Done> {
        self.buffer.push(format!("RES: {msg}"));
        Channel { buffer: self.buffer, _state: PhantomData }
    }
}

impl Channel<Done> {
    fn transcript(&self) -> &[String] {
        &self.buffer
    }
}
```

You cannot send a response before sending a request. The type system enforces protocol ordering.

### Sealed Traits

A sealed trait cannot be implemented outside your crate. This gives you freedom to add methods in future versions without breaking downstream code:

```rust
// Private module prevents external access to the supertrait
mod private {
    pub trait Sealed {}
}

/// Marker for supported database backends.
/// This trait is sealed -- it cannot be implemented outside this crate.
pub trait Backend: private::Sealed {
    fn connection_string(&self) -> String;
}

pub struct Postgres;
pub struct Sqlite;

impl private::Sealed for Postgres {}
impl private::Sealed for Sqlite {}

impl Backend for Postgres {
    fn connection_string(&self) -> String {
        "postgresql://localhost/db".into()
    }
}

impl Backend for Sqlite {
    fn connection_string(&self) -> String {
        "sqlite://data.db".into()
    }
}
```

External crates can use `Backend` as a bound, but cannot implement it. This is how the standard library seals traits like `pattern::Pattern`.

### Marker Traits

Marker traits carry no methods -- they classify types:

```rust
/// Types that are safe to serialize over the network.
/// No methods needed -- the trait itself is the statement.
trait NetworkSafe: Send + Sync + 'static {}

// Only implement for types you have audited
impl NetworkSafe for String {}
impl NetworkSafe for u64 {}
impl NetworkSafe for Vec<u8> {}
// NOT implemented for File, MutexGuard, etc.

fn broadcast<T: NetworkSafe>(msg: &T) {
    // The bound guarantees T is safe to serialize and send
    todo!()
}
```

`Send` and `Sync` are the canonical marker traits. The compiler auto-implements them, but you can opt out with `impl !Send for MyType {}` (nightly) or by including a `PhantomData<*const ()>` field (stable -- raw pointers are neither Send nor Sync).

### compile_error! and Const Assertions

`compile_error!` produces a custom compilation error:

```rust
#[cfg(all(feature = "backend-postgres", feature = "backend-sqlite"))]
compile_error!("Cannot enable both backend-postgres and backend-sqlite simultaneously");
```

Const assertions verify invariants at compile time:

```rust
const _: () = {
    assert!(std::mem::size_of::<u64>() == 8, "u64 must be 8 bytes");
    assert!(std::mem::align_of::<u64>() == 8, "u64 must be 8-byte aligned");
};

// Generic const assertion (requires a helper)
struct AssertSize<const N: usize>;
impl<const N: usize> AssertSize<N> {
    const CHECK: () = assert!(N <= 256, "Maximum size exceeded");
}

fn use_buffer<const N: usize>() {
    let _ = AssertSize::<N>::CHECK;
    let _buf = [0u8; N];
}
```

### When Not to Use Typestates

Typestates have real costs:

1. **Code duplication**: Each state transition function reconstructs the struct.
2. **Trait object incompatibility**: `Box<dyn Builder>` cannot work when the type changes at each step. You need an enum wrapper or lose the typestate.
3. **Conditional transitions**: If the next state depends on runtime data (e.g., authentication may succeed or fail), you need `Result<NextState, CurrentState>`, which complicates the API.
4. **Generic bounds propagation**: Functions that accept a `RequestBuilder<S>` must either be generic over `S` or accept a specific state.

Rule of thumb: use typestates for protocols with a small number of linear states (3-5). For complex state machines with many conditional branches, a runtime enum with `match` is often clearer.

## Exercises

### Exercise 1: Validated User Registration

Build a user registration pipeline where each field must be validated before a `User` can be constructed. Use phantom types to make it impossible to create a `User` with invalid data.

**Requirements:**
- `Username<Unvalidated>` and `Username<Validated>` (via phantom type)
- `Password<Unvalidated>` and `Password<Validated>` (via phantom type)
- `User` struct requires `Username<Validated>` + `Password<Validated>` in its constructor
- Validation rules: username 3-20 chars alphanumeric; password 8+ chars with at least one digit
- It must be impossible to construct a `User` with unvalidated fields (no `unsafe` escape)
- Write tests including compile-fail reasoning (document what would fail if attempted)

```toml
[package]
name = "validated-registration"
version = "0.1.0"
edition = "2024"
```

<details>
<summary>Solution</summary>

```rust
use std::marker::PhantomData;

// --- State markers ---
struct Unvalidated;
struct Validated;

// --- Validated wrapper ---
#[derive(Debug, Clone)]
struct Username<State = Unvalidated> {
    value: String,
    _state: PhantomData<State>,
}

#[derive(Debug, Clone)]
struct Password<State = Unvalidated> {
    value: String,
    _state: PhantomData<State>,
}

// --- Validation errors ---
#[derive(Debug, PartialEq)]
enum ValidationError {
    UsernameTooShort,
    UsernameTooLong,
    UsernameInvalidChars,
    PasswordTooShort,
    PasswordNeedsDigit,
}

impl std::fmt::Display for ValidationError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::UsernameTooShort => write!(f, "username must be at least 3 characters"),
            Self::UsernameTooLong => write!(f, "username must be at most 20 characters"),
            Self::UsernameInvalidChars => write!(f, "username must be alphanumeric"),
            Self::PasswordTooShort => write!(f, "password must be at least 8 characters"),
            Self::PasswordNeedsDigit => write!(f, "password must contain at least one digit"),
        }
    }
}

impl Username<Unvalidated> {
    fn new(value: impl Into<String>) -> Self {
        Username { value: value.into(), _state: PhantomData }
    }

    fn validate(self) -> Result<Username<Validated>, ValidationError> {
        if self.value.len() < 3 {
            return Err(ValidationError::UsernameTooShort);
        }
        if self.value.len() > 20 {
            return Err(ValidationError::UsernameTooLong);
        }
        if !self.value.chars().all(|c| c.is_ascii_alphanumeric() || c == '_') {
            return Err(ValidationError::UsernameInvalidChars);
        }
        Ok(Username { value: self.value, _state: PhantomData })
    }
}

impl Username<Validated> {
    fn as_str(&self) -> &str {
        &self.value
    }
}

impl Password<Unvalidated> {
    fn new(value: impl Into<String>) -> Self {
        Password { value: value.into(), _state: PhantomData }
    }

    fn validate(self) -> Result<Password<Validated>, ValidationError> {
        if self.value.len() < 8 {
            return Err(ValidationError::PasswordTooShort);
        }
        if !self.value.chars().any(|c| c.is_ascii_digit()) {
            return Err(ValidationError::PasswordNeedsDigit);
        }
        Ok(Password { value: self.value, _state: PhantomData })
    }
}

// --- User: requires BOTH validated ---
#[derive(Debug)]
struct User {
    username: String,
    password_hash: String,
}

impl User {
    /// The ONLY way to construct a User.
    /// Accepts only Validated variants -- enforced at compile time.
    fn register(
        username: Username<Validated>,
        password: Password<Validated>,
    ) -> Self {
        // In production, hash the password
        let password_hash = format!("hashed:{}", password.value);
        User {
            username: username.value,
            password_hash,
        }
    }
}

fn main() {
    let username = Username::new("alice_42");
    let password = Password::new("secret123");

    // Both must be validated before User::register compiles
    let valid_user = username.validate().unwrap();
    let valid_pass = password.validate().unwrap();

    let user = User::register(valid_user, valid_pass);
    println!("Registered: {:?}", user);

    // These would NOT compile (uncomment to verify):
    // let raw_user = Username::new("bob");
    // let raw_pass = Password::new("pass");
    // User::register(raw_user, raw_pass);  // Error: expected Validated, found Unvalidated
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn valid_registration() {
        let u = Username::new("alice").validate().unwrap();
        let p = Password::new("secure99").validate().unwrap();
        let user = User::register(u, p);
        assert_eq!(user.username, "alice");
    }

    #[test]
    fn username_too_short() {
        let err = Username::new("ab").validate().unwrap_err();
        assert_eq!(err, ValidationError::UsernameTooShort);
    }

    #[test]
    fn username_invalid_chars() {
        let err = Username::new("user@name").validate().unwrap_err();
        assert_eq!(err, ValidationError::UsernameInvalidChars);
    }

    #[test]
    fn password_needs_digit() {
        let err = Password::new("nodigitshere").validate().unwrap_err();
        assert_eq!(err, ValidationError::PasswordNeedsDigit);
    }

    #[test]
    fn password_too_short() {
        let err = Password::new("short1").validate().unwrap_err();
        assert_eq!(err, ValidationError::PasswordTooShort);
    }

    // Compile-time guarantee documentation:
    // The following would NOT compile:
    //
    //   let u = Username::new("alice");  // Username<Unvalidated>
    //   let p = Password::new("pass1");  // Password<Unvalidated>
    //   User::register(u, p);
    //
    // Error: expected `Username<Validated>`, found `Username<Unvalidated>`
    //
    // This is the whole point: the type system prevents unvalidated construction.
}
```
</details>

### Exercise 2: Branded Numeric Types with Arithmetic

Build a dimensional analysis system using branded types. Operations between incompatible dimensions must fail at compile time.

**Requirements:**
- `Quantity<Unit>` with phantom type for the unit
- Unit types: `Meters`, `Seconds`, `MetersPerSecond`
- Implement `Add` and `Sub` for same-unit quantities
- Implement `Div<Quantity<Seconds>>` for `Quantity<Meters>` returning `Quantity<MetersPerSecond>`
- Implement `Mul<Quantity<Seconds>>` for `Quantity<MetersPerSecond>` returning `Quantity<Meters>`
- Explicit conversion: `Quantity<Feet>::to_meters()`
- Write tests that exercise all operations and document what combinations would fail to compile

```toml
[package]
name = "branded-units"
version = "0.1.0"
edition = "2024"
```

<details>
<summary>Solution</summary>

```rust
use std::marker::PhantomData;
use std::ops::{Add, Sub, Div, Mul, Neg};
use std::fmt;

// --- Unit markers ---
struct Meters;
struct Feet;
struct Seconds;
struct MetersPerSecond;

// --- Quantity ---
#[derive(Clone, Copy)]
struct Quantity<Unit> {
    value: f64,
    _unit: PhantomData<Unit>,
}

impl<U> Quantity<U> {
    fn new(value: f64) -> Self {
        Quantity { value, _unit: PhantomData }
    }

    fn value(self) -> f64 {
        self.value
    }
}

impl<U> fmt::Debug for Quantity<U> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "Quantity({})", self.value)
    }
}

impl<U> PartialEq for Quantity<U> {
    fn eq(&self, other: &Self) -> bool {
        (self.value - other.value).abs() < 1e-10
    }
}

// --- Same-unit arithmetic ---
impl<U> Add for Quantity<U> {
    type Output = Self;
    fn add(self, rhs: Self) -> Self {
        Quantity::new(self.value + rhs.value)
    }
}

impl<U> Sub for Quantity<U> {
    type Output = Self;
    fn sub(self, rhs: Self) -> Self {
        Quantity::new(self.value - rhs.value)
    }
}

impl<U> Neg for Quantity<U> {
    type Output = Self;
    fn neg(self) -> Self {
        Quantity::new(-self.value)
    }
}

// --- Cross-unit arithmetic: distance / time = velocity ---
impl Div<Quantity<Seconds>> for Quantity<Meters> {
    type Output = Quantity<MetersPerSecond>;
    fn div(self, rhs: Quantity<Seconds>) -> Quantity<MetersPerSecond> {
        Quantity::new(self.value / rhs.value)
    }
}

// --- Cross-unit arithmetic: velocity * time = distance ---
impl Mul<Quantity<Seconds>> for Quantity<MetersPerSecond> {
    type Output = Quantity<Meters>;
    fn mul(self, rhs: Quantity<Seconds>) -> Quantity<Meters> {
        Quantity::new(self.value * rhs.value)
    }
}

// --- Explicit unit conversion ---
impl Quantity<Feet> {
    fn to_meters(self) -> Quantity<Meters> {
        Quantity::new(self.value * 0.3048)
    }
}

impl Quantity<Meters> {
    fn to_feet(self) -> Quantity<Feet> {
        Quantity::new(self.value / 0.3048)
    }
}

// --- Type aliases for ergonomics ---
type Distance = Quantity<Meters>;
type Duration = Quantity<Seconds>;
type Speed = Quantity<MetersPerSecond>;

fn main() {
    let distance: Distance = Quantity::new(100.0);
    let time: Duration = Quantity::new(9.58);
    let speed: Speed = distance / time;

    println!("Usain Bolt: {:.2} m / {:.2} s = {:.2} m/s",
        distance.value(), time.value(), speed.value());

    // Reconstruct distance from speed * time
    let reconstructed: Distance = speed * time;
    println!("Reconstructed distance: {:.2} m", reconstructed.value());

    // Unit conversion
    let feet = Quantity::<Feet>::new(328.084);
    let meters = feet.to_meters();
    println!("{:.1} feet = {:.1} meters", feet.value(), meters.value());

    // The following would NOT compile:
    // let bad = distance + time;          // Cannot add Meters + Seconds
    // let bad = distance + feet;          // Cannot add Meters + Feet (must convert)
    // let bad = time / distance;          // Seconds / Meters not defined
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn same_unit_addition() {
        let a = Quantity::<Meters>::new(10.0);
        let b = Quantity::<Meters>::new(20.0);
        assert_eq!((a + b).value(), 30.0);
    }

    #[test]
    fn same_unit_subtraction() {
        let a = Quantity::<Seconds>::new(10.0);
        let b = Quantity::<Seconds>::new(3.0);
        assert_eq!((a - b).value(), 7.0);
    }

    #[test]
    fn distance_div_time_is_velocity() {
        let d = Quantity::<Meters>::new(100.0);
        let t = Quantity::<Seconds>::new(10.0);
        let v: Speed = d / t;
        assert_eq!(v.value(), 10.0);
    }

    #[test]
    fn velocity_mul_time_is_distance() {
        let v = Quantity::<MetersPerSecond>::new(5.0);
        let t = Quantity::<Seconds>::new(20.0);
        let d: Distance = v * t;
        assert_eq!(d.value(), 100.0);
    }

    #[test]
    fn roundtrip_distance() {
        let d = Quantity::<Meters>::new(42.0);
        let t = Quantity::<Seconds>::new(6.0);
        let v = d / t;
        let d2 = v * t;
        assert!((d.value() - d2.value()).abs() < 1e-10);
    }

    #[test]
    fn feet_to_meters_conversion() {
        let feet = Quantity::<Feet>::new(1.0);
        let meters = feet.to_meters();
        assert!((meters.value() - 0.3048).abs() < 1e-10);
    }

    #[test]
    fn meters_to_feet_to_meters_roundtrip() {
        let original = Quantity::<Meters>::new(100.0);
        let roundtrip = original.to_feet().to_meters();
        assert!((original.value() - roundtrip.value()).abs() < 1e-10);
    }

    // Compile-time guarantees (would NOT compile):
    //
    // fn bad_add() {
    //     let m = Quantity::<Meters>::new(1.0);
    //     let s = Quantity::<Seconds>::new(1.0);
    //     let _ = m + s; // Error: expected Meters, found Seconds
    // }
    //
    // fn bad_cross_unit() {
    //     let s = Quantity::<Seconds>::new(1.0);
    //     let m = Quantity::<Meters>::new(1.0);
    //     let _ = s / m; // Error: Div<Quantity<Meters>> not implemented for Quantity<Seconds>
    // }
}
```
</details>

### Exercise 3: TCP Connection Typestate

Model a TCP connection lifecycle using typestates. The connection must go through states: `Closed` -> `Connecting` -> `Connected` -> `Closed`. Data can only be sent/received in the `Connected` state. Closing a `Connected` connection yields a `Closed` connection (not void).

**Requirements:**
- Four states: `Closed`, `Connecting`, `Connected`, `Listening` (for server)
- `TcpConnection<Closed>` can `.connect(addr)` -> `Result<TcpConnection<Connected>, TcpConnection<Closed>>`
- `TcpConnection<Closed>` can `.listen(port)` -> `TcpConnection<Listening>`
- `TcpConnection<Listening>` can `.accept()` -> `(TcpConnection<Connected>, TcpConnection<Listening>)`
- `TcpConnection<Connected>` can `.send()`, `.recv()`, `.close()` -> `TcpConnection<Closed>`
- Implement `Drop` only for `Connected` (auto-close with warning)
- Write tests that verify the full lifecycle

```toml
[package]
name = "tcp-typestate"
version = "0.1.0"
edition = "2024"
```

<details>
<summary>Solution</summary>

```rust
use std::marker::PhantomData;

// --- States ---
struct Closed;
struct Connecting;
struct Connected;
struct Listening;

// --- Connection ---
struct TcpConnection<State> {
    addr: String,
    bytes_sent: usize,
    bytes_recv: usize,
    _state: PhantomData<State>,
}

impl TcpConnection<Closed> {
    fn new() -> Self {
        TcpConnection {
            addr: String::new(),
            bytes_sent: 0,
            bytes_recv: 0,
            _state: PhantomData,
        }
    }

    fn connect(self, addr: &str) -> Result<TcpConnection<Connected>, TcpConnection<Closed>> {
        // Simulate: fail if addr is "fail"
        if addr == "fail" {
            return Err(TcpConnection {
                addr: addr.to_string(),
                bytes_sent: 0,
                bytes_recv: 0,
                _state: PhantomData,
            });
        }

        println!("[TCP] Connected to {addr}");
        Ok(TcpConnection {
            addr: addr.to_string(),
            bytes_sent: 0,
            bytes_recv: 0,
            _state: PhantomData,
        })
    }

    fn listen(self, port: u16) -> TcpConnection<Listening> {
        let addr = format!("0.0.0.0:{port}");
        println!("[TCP] Listening on {addr}");
        TcpConnection {
            addr,
            bytes_sent: 0,
            bytes_recv: 0,
            _state: PhantomData,
        }
    }
}

impl TcpConnection<Listening> {
    fn accept(&self) -> TcpConnection<Connected> {
        let client_addr = format!("client->{}", self.addr);
        println!("[TCP] Accepted connection: {client_addr}");
        TcpConnection {
            addr: client_addr,
            bytes_sent: 0,
            bytes_recv: 0,
            _state: PhantomData,
        }
    }

    fn close(self) -> TcpConnection<Closed> {
        println!("[TCP] Stopped listening on {}", self.addr);
        TcpConnection {
            addr: String::new(),
            bytes_sent: 0,
            bytes_recv: 0,
            _state: PhantomData,
        }
    }
}

impl TcpConnection<Connected> {
    fn send(&mut self, data: &[u8]) -> usize {
        self.bytes_sent += data.len();
        println!("[TCP] Sent {} bytes to {}", data.len(), self.addr);
        data.len()
    }

    fn recv(&mut self, buf: &mut [u8]) -> usize {
        // Simulate: fill with incrementing bytes
        let n = buf.len().min(64);
        for (i, byte) in buf.iter_mut().enumerate().take(n) {
            *byte = (i % 256) as u8;
        }
        self.bytes_recv += n;
        println!("[TCP] Received {n} bytes from {}", self.addr);
        n
    }

    fn close(self) -> TcpConnection<Closed> {
        println!(
            "[TCP] Closing connection to {} (sent={}, recv={})",
            self.addr, self.bytes_sent, self.bytes_recv
        );
        // Prevent Drop from also running
        let addr = self.addr.clone();
        std::mem::forget(self);
        TcpConnection {
            addr,
            bytes_sent: 0,
            bytes_recv: 0,
            _state: PhantomData,
        }
    }

    fn stats(&self) -> (usize, usize) {
        (self.bytes_sent, self.bytes_recv)
    }
}

impl Drop for TcpConnection<Connected> {
    fn drop(&mut self) {
        eprintln!(
            "[TCP] WARNING: connection to {} dropped without explicit close \
             (sent={}, recv={})",
            self.addr, self.bytes_sent, self.bytes_recv
        );
    }
}

fn main() {
    // Client flow
    let conn = TcpConnection::new();
    let mut conn = conn.connect("192.168.1.1:8080").unwrap();
    conn.send(b"GET / HTTP/1.1\r\n");
    let mut buf = [0u8; 32];
    conn.recv(&mut buf);
    let _closed = conn.close();  // Explicit close, no Drop warning

    println!();

    // Server flow
    let server = TcpConnection::new();
    let listener = server.listen(8080);
    let mut client = listener.accept();
    client.send(b"HTTP/1.1 200 OK\r\n");
    let _closed_client = client.close();
    let _closed_server = listener.close();

    println!();

    // Failed connection returns Closed state (can retry)
    let conn = TcpConnection::new();
    match conn.connect("fail") {
        Ok(_) => unreachable!(),
        Err(closed) => {
            println!("Connection failed, retrying...");
            let _retry = closed.connect("192.168.1.1:8080");
        }
    }

    // These would NOT compile:
    // let c = TcpConnection::new();
    // c.send(b"data");          // Error: no method `send` on TcpConnection<Closed>
    // c.recv(&mut [0u8; 10]);   // Error: no method `recv` on TcpConnection<Closed>
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn client_lifecycle() {
        let conn = TcpConnection::new();
        let mut conn = conn.connect("127.0.0.1:80").unwrap();
        let sent = conn.send(b"hello");
        assert_eq!(sent, 5);
        let (s, _r) = conn.stats();
        assert_eq!(s, 5);
        let _closed = conn.close();
    }

    #[test]
    fn server_lifecycle() {
        let listener = TcpConnection::new().listen(9090);
        let mut client = listener.accept();
        client.send(b"response");
        let (s, _) = client.stats();
        assert_eq!(s, 8);
        let _c = client.close();
        let _l = listener.close();
    }

    #[test]
    fn failed_connect_returns_closed() {
        let conn = TcpConnection::new();
        let err = conn.connect("fail").unwrap_err();
        // The returned Closed connection can be used to retry
        let ok = err.connect("127.0.0.1:80");
        assert!(ok.is_ok());
        let _c = ok.unwrap().close();
    }

    #[test]
    fn recv_fills_buffer() {
        let mut conn = TcpConnection::new().connect("localhost:80").unwrap();
        let mut buf = [0u8; 16];
        let n = conn.recv(&mut buf);
        assert_eq!(n, 16);
        assert_eq!(buf[0], 0);
        assert_eq!(buf[1], 1);
        let _c = conn.close();
    }

    #[test]
    fn cumulative_stats() {
        let mut conn = TcpConnection::new().connect("localhost:80").unwrap();
        conn.send(b"aaa");
        conn.send(b"bbbbb");
        let (sent, _) = conn.stats();
        assert_eq!(sent, 8);
        let _c = conn.close();
    }
}
```
</details>

### Exercise 4: Sealed Trait with Const Assertions

Design a `Codec` sealed trait for a serialization library. Only your crate's types (`Json`, `Msgpack`, `Cbor`) can implement it. Each codec has a compile-time maximum message size enforced via const generics.

**Requirements:**
- Sealed trait pattern (private module with `Sealed` supertrait)
- `Codec` trait with `const MAX_SIZE: usize`, `fn encode(&self, value: &str) -> Vec<u8>`, `fn decode(&self, bytes: &[u8]) -> String`
- Three implementors: `Json`, `Msgpack`, `Cbor` (simulated encoding is fine)
- Const assertion: if `MAX_SIZE < 64`, compilation fails
- A generic function `transfer<C: Codec>(codec: &C, msg: &str)` that uses the trait
- Write tests proving the sealed trait prevents external impls (document in comments)

```toml
[package]
name = "sealed-codec"
version = "0.1.0"
edition = "2024"
```

<details>
<summary>Solution</summary>

```rust
mod private {
    pub trait Sealed {}
}

/// Sealed codec trait. Cannot be implemented outside this crate.
pub trait Codec: private::Sealed {
    const MAX_SIZE: usize;
    const NAME: &'static str;

    fn encode(&self, value: &str) -> Vec<u8>;
    fn decode(&self, bytes: &[u8]) -> String;
}

// --- Const assertion helper ---
struct AssertMinSize<const N: usize>;
impl<const N: usize> AssertMinSize<N> {
    const CHECK: () = assert!(N >= 64, "MAX_SIZE must be at least 64 bytes");
}

// --- Codec implementations ---

pub struct Json;
pub struct Msgpack;
pub struct Cbor;

impl private::Sealed for Json {}
impl private::Sealed for Msgpack {}
impl private::Sealed for Cbor {}

impl Codec for Json {
    const MAX_SIZE: usize = 1_048_576; // 1 MiB
    const NAME: &'static str = "JSON";

    fn encode(&self, value: &str) -> Vec<u8> {
        // Simulated JSON encoding: wrap in quotes
        format!("\"{value}\"").into_bytes()
    }

    fn decode(&self, bytes: &[u8]) -> String {
        let s = std::str::from_utf8(bytes).unwrap_or("");
        s.trim_matches('"').to_string()
    }
}

impl Codec for Msgpack {
    const MAX_SIZE: usize = 4_194_304; // 4 MiB
    const NAME: &'static str = "MessagePack";

    fn encode(&self, value: &str) -> Vec<u8> {
        // Simulated: length prefix + raw bytes
        let len = value.len() as u8;
        let mut out = vec![0xA0 | len.min(31)];
        out.extend_from_slice(value.as_bytes());
        out
    }

    fn decode(&self, bytes: &[u8]) -> String {
        if bytes.is_empty() {
            return String::new();
        }
        // Skip the type/length byte
        String::from_utf8_lossy(&bytes[1..]).to_string()
    }
}

impl Codec for Cbor {
    const MAX_SIZE: usize = 65_536; // 64 KiB
    const NAME: &'static str = "CBOR";

    fn encode(&self, value: &str) -> Vec<u8> {
        // Simulated: major type 3 (text string) + length + bytes
        let len = value.len() as u8;
        let mut out = vec![0x60 | len.min(23)];
        out.extend_from_slice(value.as_bytes());
        out
    }

    fn decode(&self, bytes: &[u8]) -> String {
        if bytes.is_empty() {
            return String::new();
        }
        String::from_utf8_lossy(&bytes[1..]).to_string()
    }
}

// --- Const assertions fire at monomorphization ---
fn transfer<C: Codec>(codec: &C, msg: &str) -> Vec<u8> {
    // This line triggers the const assertion at compile time
    let _ = AssertMinSize::<{ C::MAX_SIZE }>::CHECK;

    let encoded = codec.encode(msg);
    assert!(
        encoded.len() <= C::MAX_SIZE,
        "message exceeds {} max size of {} bytes",
        C::NAME,
        C::MAX_SIZE,
    );
    println!(
        "[{}] Encoded {} bytes (max {})",
        C::NAME,
        encoded.len(),
        C::MAX_SIZE,
    );
    encoded
}

fn roundtrip<C: Codec>(codec: &C, msg: &str) -> bool {
    let encoded = codec.encode(msg);
    let decoded = codec.decode(&encoded);
    decoded == msg
}

fn main() {
    let msg = "hello, world";

    let json_bytes = transfer(&Json, msg);
    let msgpack_bytes = transfer(&Msgpack, msg);
    let cbor_bytes = transfer(&Cbor, msg);

    println!("JSON:    {} bytes", json_bytes.len());
    println!("Msgpack: {} bytes", msgpack_bytes.len());
    println!("CBOR:    {} bytes", cbor_bytes.len());

    // Uncomment to trigger const assertion failure:
    // struct TinyCodec;
    // impl private::Sealed for TinyCodec {} // Won't compile: private module
    // impl Codec for TinyCodec {
    //     const MAX_SIZE: usize = 32;  // Would fail const assertion
    //     const NAME: &'static str = "Tiny";
    //     fn encode(&self, _: &str) -> Vec<u8> { vec![] }
    //     fn decode(&self, _: &[u8]) -> String { String::new() }
    // }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn json_roundtrip() {
        assert!(roundtrip(&Json, "test value"));
    }

    #[test]
    fn msgpack_roundtrip() {
        assert!(roundtrip(&Msgpack, "test value"));
    }

    #[test]
    fn cbor_roundtrip() {
        assert!(roundtrip(&Cbor, "test value"));
    }

    #[test]
    fn transfer_works_for_all_codecs() {
        let msg = "integration test";
        let _ = transfer(&Json, msg);
        let _ = transfer(&Msgpack, msg);
        let _ = transfer(&Cbor, msg);
    }

    #[test]
    fn codec_names_are_correct() {
        assert_eq!(Json::NAME, "JSON");
        assert_eq!(Msgpack::NAME, "MessagePack");
        assert_eq!(Cbor::NAME, "CBOR");
    }

    #[test]
    fn max_sizes_are_reasonable() {
        assert!(Json::MAX_SIZE >= 64);
        assert!(Msgpack::MAX_SIZE >= 64);
        assert!(Cbor::MAX_SIZE >= 64);
    }

    // Sealed trait guarantee (would NOT compile):
    //
    // struct ExternalCodec;
    // impl private::Sealed for ExternalCodec {}
    //     ^^^^^^^
    //     error[E0603]: module `private` is private
    //
    // impl Codec for ExternalCodec { ... }
    //     ^^^^^
    //     error[E0277]: the trait bound `ExternalCodec: Sealed` is not satisfied
}
```
</details>

### Exercise 5: Production Typestate -- Payment Pipeline

Build a payment processing pipeline where a payment goes through: `Created` -> `Authorized` -> `Captured` -> `Settled`. Alternatively, from `Authorized` it can go to `Voided`. From `Captured` it can go to `Refunded`. Each state transition returns a new typed state. Include idempotency tokens and audit logging.

This exercise combines phantom types, sealed traits, and const assertions into a realistic production pattern.

**Requirements:**
- Five states: `Created`, `Authorized`, `Captured`, `Settled`, `Voided`, `Refunded`
- `Payment<Created>` -> `.authorize()` -> `Result<Payment<Authorized>, PaymentError>`
- `Payment<Authorized>` -> `.capture()` or `.void()`
- `Payment<Captured>` -> `.settle()` or `.refund(reason)`
- Each state carries different data (auth_code for Authorized, capture_id for Captured, etc.)
- Sealed `PaymentState` trait prevents external states
- Audit log: each transition appends to a `Vec<AuditEntry>`
- Idempotency: calling `.authorize()` twice on the same payment ID returns the same result (simulate with a static map)
- Write comprehensive tests covering the happy path and all alternative flows

```toml
[package]
name = "payment-typestate"
version = "0.1.0"
edition = "2024"
```

<details>
<summary>Solution</summary>

```rust
use std::marker::PhantomData;
use std::collections::HashMap;
use std::sync::Mutex;
use std::sync::LazyLock;

// --- Sealed trait for states ---
mod private {
    pub trait Sealed {}
}

trait PaymentState: private::Sealed {
    const NAME: &'static str;
}

// --- States ---
struct Created;
struct Authorized;
struct Captured;
struct Settled;
struct Voided;
struct Refunded;

impl private::Sealed for Created {}
impl private::Sealed for Authorized {}
impl private::Sealed for Captured {}
impl private::Sealed for Settled {}
impl private::Sealed for Voided {}
impl private::Sealed for Refunded {}

impl PaymentState for Created { const NAME: &'static str = "Created"; }
impl PaymentState for Authorized { const NAME: &'static str = "Authorized"; }
impl PaymentState for Captured { const NAME: &'static str = "Captured"; }
impl PaymentState for Settled { const NAME: &'static str = "Settled"; }
impl PaymentState for Voided { const NAME: &'static str = "Voided"; }
impl PaymentState for Refunded { const NAME: &'static str = "Refunded"; }

// --- Audit log ---
#[derive(Debug, Clone)]
struct AuditEntry {
    from: &'static str,
    to: &'static str,
    detail: String,
    timestamp: u64,
}

// --- Payment error ---
#[derive(Debug)]
enum PaymentError {
    AuthorizationDeclined(String),
    CaptureExceedsAuth { authorized: u64, requested: u64 },
    AlreadyProcessed(String),
}

impl std::fmt::Display for PaymentError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::AuthorizationDeclined(msg) => write!(f, "authorization declined: {msg}"),
            Self::CaptureExceedsAuth { authorized, requested } => {
                write!(f, "capture {requested} exceeds auth {authorized}")
            }
            Self::AlreadyProcessed(id) => write!(f, "payment {id} already processed"),
        }
    }
}

// --- Idempotency store (simulated) ---
static IDEMPOTENCY: LazyLock<Mutex<HashMap<String, String>>> =
    LazyLock::new(|| Mutex::new(HashMap::new()));

fn next_timestamp() -> u64 {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64
}

// --- Payment struct ---
struct Payment<State: PaymentState> {
    id: String,
    amount_cents: u64,
    currency: String,
    auth_code: Option<String>,
    capture_id: Option<String>,
    refund_reason: Option<String>,
    audit_log: Vec<AuditEntry>,
    _state: PhantomData<State>,
}

impl Payment<Created> {
    fn new(id: impl Into<String>, amount_cents: u64, currency: impl Into<String>) -> Self {
        let id = id.into();
        let currency = currency.into();
        let mut payment = Payment {
            id: id.clone(),
            amount_cents,
            currency,
            auth_code: None,
            capture_id: None,
            refund_reason: None,
            audit_log: Vec::new(),
            _state: PhantomData,
        };
        payment.audit_log.push(AuditEntry {
            from: "None",
            to: Created::NAME,
            detail: format!("Payment {} created for {} cents", id, amount_cents),
            timestamp: next_timestamp(),
        });
        payment
    }

    fn authorize(mut self) -> Result<Payment<Authorized>, PaymentError> {
        // Idempotency check
        let mut store = IDEMPOTENCY.lock().unwrap();
        if let Some(existing_code) = store.get(&self.id) {
            // Already authorized -- return same result
            self.auth_code = Some(existing_code.clone());
            self.audit_log.push(AuditEntry {
                from: Created::NAME,
                to: Authorized::NAME,
                detail: format!("Idempotent re-authorization: {existing_code}"),
                timestamp: next_timestamp(),
            });
            return Ok(Payment {
                id: self.id,
                amount_cents: self.amount_cents,
                currency: self.currency,
                auth_code: self.auth_code,
                capture_id: self.capture_id,
                refund_reason: self.refund_reason,
                audit_log: self.audit_log,
                _state: PhantomData,
            });
        }

        // Simulate: decline amounts over 1_000_000
        if self.amount_cents > 1_000_000 {
            return Err(PaymentError::AuthorizationDeclined(
                format!("amount {} exceeds limit", self.amount_cents),
            ));
        }

        let auth_code = format!("AUTH-{}-{}", self.id, self.amount_cents);
        store.insert(self.id.clone(), auth_code.clone());

        self.auth_code = Some(auth_code.clone());
        self.audit_log.push(AuditEntry {
            from: Created::NAME,
            to: Authorized::NAME,
            detail: format!("Authorized with code {auth_code}"),
            timestamp: next_timestamp(),
        });

        Ok(Payment {
            id: self.id,
            amount_cents: self.amount_cents,
            currency: self.currency,
            auth_code: self.auth_code,
            capture_id: self.capture_id,
            refund_reason: self.refund_reason,
            audit_log: self.audit_log,
            _state: PhantomData,
        })
    }
}

impl Payment<Authorized> {
    fn capture(mut self, amount_cents: u64) -> Result<Payment<Captured>, PaymentError> {
        if amount_cents > self.amount_cents {
            return Err(PaymentError::CaptureExceedsAuth {
                authorized: self.amount_cents,
                requested: amount_cents,
            });
        }

        let capture_id = format!("CAP-{}-{}", self.id, amount_cents);
        self.capture_id = Some(capture_id.clone());
        self.audit_log.push(AuditEntry {
            from: Authorized::NAME,
            to: Captured::NAME,
            detail: format!("Captured {amount_cents} cents ({capture_id})"),
            timestamp: next_timestamp(),
        });

        Ok(Payment {
            id: self.id,
            amount_cents: amount_cents, // captured amount may be less
            currency: self.currency,
            auth_code: self.auth_code,
            capture_id: self.capture_id,
            refund_reason: self.refund_reason,
            audit_log: self.audit_log,
            _state: PhantomData,
        })
    }

    fn void(mut self) -> Payment<Voided> {
        self.audit_log.push(AuditEntry {
            from: Authorized::NAME,
            to: Voided::NAME,
            detail: format!("Authorization voided for {}", self.id),
            timestamp: next_timestamp(),
        });

        Payment {
            id: self.id,
            amount_cents: self.amount_cents,
            currency: self.currency,
            auth_code: self.auth_code,
            capture_id: self.capture_id,
            refund_reason: self.refund_reason,
            audit_log: self.audit_log,
            _state: PhantomData,
        }
    }
}

impl Payment<Captured> {
    fn settle(mut self) -> Payment<Settled> {
        self.audit_log.push(AuditEntry {
            from: Captured::NAME,
            to: Settled::NAME,
            detail: format!("Settled {} cents for {}", self.amount_cents, self.id),
            timestamp: next_timestamp(),
        });

        Payment {
            id: self.id,
            amount_cents: self.amount_cents,
            currency: self.currency,
            auth_code: self.auth_code,
            capture_id: self.capture_id,
            refund_reason: self.refund_reason,
            audit_log: self.audit_log,
            _state: PhantomData,
        }
    }

    fn refund(mut self, reason: &str) -> Payment<Refunded> {
        self.refund_reason = Some(reason.to_string());
        self.audit_log.push(AuditEntry {
            from: Captured::NAME,
            to: Refunded::NAME,
            detail: format!("Refunded {} cents: {reason}", self.amount_cents),
            timestamp: next_timestamp(),
        });

        Payment {
            id: self.id,
            amount_cents: self.amount_cents,
            currency: self.currency,
            auth_code: self.auth_code,
            capture_id: self.capture_id,
            refund_reason: self.refund_reason,
            audit_log: self.audit_log,
            _state: PhantomData,
        }
    }
}

// --- Common methods for all terminal states ---
impl<S: PaymentState> Payment<S> {
    fn id(&self) -> &str {
        &self.id
    }

    fn audit_log(&self) -> &[AuditEntry] {
        &self.audit_log
    }

    fn state_name(&self) -> &'static str {
        S::NAME
    }
}

fn main() {
    // Happy path: Created -> Authorized -> Captured -> Settled
    let payment = Payment::new("PAY-001", 5000, "USD");
    let payment = payment.authorize().unwrap();
    let payment = payment.capture(5000).unwrap();
    let payment = payment.settle();

    println!("Payment {} is now {}", payment.id(), payment.state_name());
    for entry in payment.audit_log() {
        println!("  [{} -> {}] {}", entry.from, entry.to, entry.detail);
    }

    println!();

    // Alternative: void after authorization
    let payment = Payment::new("PAY-002", 3000, "USD");
    let payment = payment.authorize().unwrap();
    let payment = payment.void();
    println!("Payment {} is now {}", payment.id(), payment.state_name());

    println!();

    // Alternative: refund after capture
    let payment = Payment::new("PAY-003", 7500, "EUR");
    let payment = payment.authorize().unwrap();
    let payment = payment.capture(7500).unwrap();
    let payment = payment.refund("customer request");
    println!("Payment {} is now {}: {:?}",
        payment.id(), payment.state_name(), payment.refund_reason);

    // The following would NOT compile:
    // let p = Payment::new("X", 100, "USD");
    // p.capture(100);    // Error: no method `capture` on Payment<Created>
    // p.settle();        // Error: no method `settle` on Payment<Created>
    //
    // let auth = Payment::new("Y", 100, "USD").authorize().unwrap();
    // auth.settle();     // Error: no method `settle` on Payment<Authorized>
    // auth.refund("x");  // Error: no method `refund` on Payment<Authorized>
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn happy_path_to_settled() {
        let p = Payment::new("test-1", 1000, "USD");
        let p = p.authorize().unwrap();
        let p = p.capture(1000).unwrap();
        let p = p.settle();
        assert_eq!(p.state_name(), "Settled");
        assert_eq!(p.audit_log().len(), 4); // created + auth + capture + settle
    }

    #[test]
    fn void_after_auth() {
        let p = Payment::new("test-2", 2000, "USD");
        let p = p.authorize().unwrap();
        let p = p.void();
        assert_eq!(p.state_name(), "Voided");
    }

    #[test]
    fn refund_after_capture() {
        let p = Payment::new("test-3", 3000, "EUR");
        let p = p.authorize().unwrap();
        let p = p.capture(3000).unwrap();
        let p = p.refund("duplicate charge");
        assert_eq!(p.state_name(), "Refunded");
        assert_eq!(p.refund_reason.as_deref(), Some("duplicate charge"));
    }

    #[test]
    fn partial_capture() {
        let p = Payment::new("test-4", 5000, "USD");
        let p = p.authorize().unwrap();
        let p = p.capture(3000).unwrap(); // partial
        assert_eq!(p.amount_cents, 3000);
        let p = p.settle();
        assert_eq!(p.state_name(), "Settled");
    }

    #[test]
    fn capture_exceeds_auth_fails() {
        let p = Payment::new("test-5", 1000, "USD");
        let p = p.authorize().unwrap();
        let err = p.capture(2000).unwrap_err();
        match err {
            PaymentError::CaptureExceedsAuth { authorized, requested } => {
                assert_eq!(authorized, 1000);
                assert_eq!(requested, 2000);
            }
            _ => panic!("expected CaptureExceedsAuth"),
        }
    }

    #[test]
    fn high_amount_declined() {
        let p = Payment::new("test-6", 2_000_000, "USD");
        let err = p.authorize().unwrap_err();
        matches!(err, PaymentError::AuthorizationDeclined(_));
    }

    #[test]
    fn audit_log_tracks_all_transitions() {
        let p = Payment::new("test-7", 100, "USD");
        assert_eq!(p.audit_log().len(), 1);

        let p = p.authorize().unwrap();
        assert_eq!(p.audit_log().len(), 2);

        let p = p.capture(100).unwrap();
        assert_eq!(p.audit_log().len(), 3);

        let p = p.settle();
        assert_eq!(p.audit_log().len(), 4);

        // Verify transition names
        assert_eq!(p.audit_log()[1].from, "Created");
        assert_eq!(p.audit_log()[1].to, "Authorized");
        assert_eq!(p.audit_log()[2].from, "Authorized");
        assert_eq!(p.audit_log()[2].to, "Captured");
    }
}
```
</details>

## Common Mistakes

1. **Over-encoding state.** Not every boolean deserves a phantom type. If the state has 2 values and is checked once, a runtime `bool` is simpler and clearer.

2. **Typestate struct reconstruction boilerplate.** Each transition copies all fields. Consider a macro or inner struct to reduce repetition. Some projects use a `StateData` inner struct that is moved between typed wrappers.

3. **Forgetting `PhantomData` variance.** `PhantomData<T>` is covariant over `T`. If you need invariance (rare), use `PhantomData<fn(T) -> T>`.

4. **Sealed traits with `pub` on the private module.** The inner `Sealed` trait must be in a truly private module (`mod private`, not `pub mod private`), or the seal is broken.

5. **Using typestates with trait objects.** `Box<dyn Builder>` erases the type parameter, defeating the purpose. If you need dynamic dispatch, typestates are the wrong tool.

## Verification

```bash
cargo test                     # All exercises
cargo clippy -- -D warnings    # No warnings
```

## Summary

The Rust type system is a proof engine. Phantom types, branded types, typestates, sealed traits, and const assertions let you encode invariants that the compiler checks exhaustively at zero runtime cost. The trade-off is API complexity -- use these techniques for invariants where runtime violations would be costly (security, protocol correctness, unit safety), not for every boolean flag.

## What's Next

Exercise 18 explores property-based testing -- a complementary approach where instead of proving invariants at compile time, you generate thousands of random inputs to find violations at test time.

## Resources

- [The Typestate Pattern in Rust](https://cliffle.com/blog/rust-typestate/) -- Cliff Biffle's definitive article
- [Rust API Guidelines: Sealed Traits](https://rust-lang.github.io/api-guidelines/future-proofing.html) -- official guidelines
- [Session Types in Rust](https://munksgaard.me/papers/laumann-munksgaard-larsen.pdf) -- academic paper
- [PhantomData documentation](https://doc.rust-lang.org/std/marker/struct.PhantomData.html) -- variance and drop check
- [compile_error! macro](https://doc.rust-lang.org/std/macro.compile_error.html) -- conditional compilation errors
