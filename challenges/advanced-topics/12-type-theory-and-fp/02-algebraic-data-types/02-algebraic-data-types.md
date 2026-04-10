<!--
type: reference
difficulty: insane
section: [12-type-theory-and-fp]
concepts: [product-types, sum-types, unit-type, void-type, never-type, church-encoding, exhaustiveness-checking, pattern-matching, tagged-unions]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: evaluate
prerequisites: [go-interfaces, rust-enums, basic-type-systems]
papers: [Reynolds-1974-relational-interpretation, Wadler-1987-views, TAPL-Pierce-2002]
industry_use: [Rust-stdlib-Option-Result, TypeScript-discriminated-unions, Elm, Haskell, OCaml]
language_contrast: high
-->

# Algebraic Data Types

> Types are sets. Product types are Cartesian products (all fields at once). Sum types are disjoint unions (exactly one variant). Exhaustiveness checking is just set coverage.

## Mental Model

A `struct` with two fields is a product type. If field A has 3 possible values and field B has 5, the struct has 3 × 5 = 15 possible states. That is why it is called a product. A `bool` combined with a `bool` in a struct: 2 × 2 = 4 possible values.

A sum type is the other fundamental building block. If you have a type that is *either* an `Int` *or* a `String` *or* an `Error`, the number of possible values is the *sum*: |Int| + |String| + |Error|. That is why it is called a sum type. In C, the unsafe version is a `union`. In Rust, it is an `enum`. In Go, it is an `interface{}` or a discriminated struct pattern.

The "algebraic" in ADT refers to this arithmetic. Types form an algebra where:
- `()` (unit) is 1 — exactly one inhabitant
- `!` or `Never` is 0 — no inhabitants (useful as the return type of `panic!`, `exit()`, or infinite loops)
- `A × B` (product) multiplies the state space
- `A + B` (sum) adds the state space

The insight that makes this powerful: once you know a value's type, the exhaustiveness checker knows exactly which variants are possible and requires you to handle all of them. Rust's `match` is sound because the type system knows the complete set of variants. Go's type switch cannot guarantee exhaustiveness because interfaces are open — any type can satisfy them.

**Practical consequence**: `Option<T>` forces you to handle `None` before you can use the value. You cannot forget. `Result<T, E>` forces you to handle the error. The famous Rust slogan "if it compiles, it probably works" is largely a consequence of this: the ADT design makes illegal states unrepresentable. If your type system cannot express a state, your program cannot enter it.

## Core Concepts

### Product Types

Product types encode "and" — a value of type `A × B` has *both* an A component and a B component, simultaneously. The canonical product type is the struct.

```
type Point = { x: Float, y: Float }  // has both x and y
```

Two special cases:
- **Unit type** (written `()` in Rust, `struct{}` in Go): a product type with zero fields. It has exactly one inhabitant. It is used as the return type of functions that produce no meaningful value (Go's implicit `void` is morally this).
- **Pair** `(A, B)`: the simplest non-trivial product. Generalizes to tuples.

### Sum Types

Sum types encode "or" — a value of type `A + B` is *either* an A *or* a B, and you can always tell which one it is (the tag). The canonical sum type is the tagged union / discriminated union.

```
type Shape = Circle { radius: Float }
           | Rectangle { width: Float, height: Float }
           | Triangle { base: Float, height: Float }
```

A `Shape` value is exactly one of the three variants. The exhaustiveness check ensures every `match` on a `Shape` covers all three. Add a fourth variant, `Pentagon`? The compiler shows you every place in the codebase that now has a non-exhaustive match.

Two special cases:
- **Bool**: exactly `True + False`. Two variants, no payload. The simplest non-trivial sum.
- **Never / `!` / Void**: a sum type with *zero* variants. It has no inhabitants. A function returning `Never` never returns (it panics, loops forever, or calls `exit`). This is different from `()` — unit returns normally, carrying no information; `Never` does not return at all.

### Option and Result as ADTs

`Option<T>` is `T + ()` with better names: `Some(T) + None`. It is the sum of "a value of type T" and "the absence of a value."

`Result<T, E>` is `T + E` with better names: `Ok(T) + Err(E)`. It is the sum of "success with value T" and "failure with error E."

These are not special language features. They are regular ADTs. The exhaustiveness checking is not built into `Option` or `Result` — it falls out of the match statement's general exhaustiveness checking applied to any sum type.

### Church Encoding (Functions as Data)

Church encoding shows that you can represent any data type as a pure function. It is not a practical pattern in Go or Rust — it is an illuminating demonstration that data and computation are more closely related than they appear.

A boolean: instead of `true` and `false`, represent them as functions that choose between two arguments:
```
true  = λt.λf.t   // returns the first argument
false = λt.λf.f   // returns the second argument
```

The `if` expression becomes function application:
```
if b then x else y = b(x)(y)
```

A natural number N is represented as "apply f to x N times":
```
zero  = λf.λx.x
one   = λf.λx.f(x)
two   = λf.λx.f(f(x))
succ  = λn.λf.λx.f(n(f)(x))
```

This encoding matters for one practical reason: it shows that the type structure of your data *is* an interface. `Option<T>` as a Church encoding is exactly the visitor pattern — a function `<R>(onSome: (T) => R, onNone: () => R) => R`. Understanding this makes the relationship between ADTs and the visitor pattern obvious.

## Implementation: Go

```go
package main

import "fmt"

// ─── Product Types ────────────────────────────────────────────────────────────

// Point is a product type: it always has both X and Y.
// Possible states: |float64| × |float64|
type Point struct {
	X, Y float64
}

// Unit is the product type with zero fields — one inhabitant.
// In Go, struct{} is used as a signal value (done channels, sets).
type Unit = struct{}

// ─── Sum Types via Sealed Interfaces ─────────────────────────────────────────
//
// Go does not have native sum types. The idiomatic simulation:
// 1. Define a marker interface with an unexported method.
// 2. All variants implement it in the same package (sealing the sum).
// 3. Use type switch for pattern matching (not exhaustive — see limitations).

// Shape is a sum type: Circle | Rectangle | Triangle.
// The unexported shape() method seals it to this package.
type Shape interface {
	shape()
	Area() float64
}

type Circle struct{ Radius float64 }
type Rectangle struct{ Width, Height float64 }
type Triangle struct{ Base, Height float64 }

func (Circle) shape()              {}
func (Rectangle) shape()          {}
func (Triangle) shape()           {}
func (c Circle) Area() float64    { return 3.14159 * c.Radius * c.Radius }
func (r Rectangle) Area() float64 { return r.Width * r.Height }
func (t Triangle) Area() float64  { return 0.5 * t.Base * t.Height }

// describe performs pattern matching via type switch.
// LIMITATION: The compiler does NOT check exhaustiveness.
// If we add Pentagon, this compiles silently and returns "unknown".
func describe(s Shape) string {
	switch v := s.(type) {
	case Circle:
		return fmt.Sprintf("circle with radius %.2f, area %.2f", v.Radius, v.Area())
	case Rectangle:
		return fmt.Sprintf("rectangle %gx%g, area %.2f", v.Width, v.Height, v.Area())
	case Triangle:
		return fmt.Sprintf("triangle base=%g height=%g, area %.2f", v.Base, v.Height, v.Area())
	default:
		// This branch SILENTLY handles unknown variants.
		// In Rust, adding a variant forces a compile error here.
		return "unknown shape"
	}
}

// ─── Option<T> Simulation ─────────────────────────────────────────────────────

// Option[T] is the sum type T + () (some value or nothing).
// Without generics, Go historically used (T, bool) or (T, error).
// With generics (1.18+):
type Option[T any] struct {
	value  T
	isSome bool
}

func Some[T any](v T) Option[T]  { return Option[T]{v, true} }
func None[T any]() Option[T]    { return Option[T]{} }

// Match provides exhaustive handling — the caller must supply both branches.
// This is the Church encoding: Option is a function that takes two handlers.
func (o Option[T]) Match(onSome func(T), onNone func()) {
	if o.isSome {
		onSome(o.value)
	} else {
		onNone()
	}
}

// Map applies f to the contained value, if present.
func (o Option[T]) Map(f func(T) T) Option[T] {
	if o.isSome {
		return Some(f(o.value))
	}
	return o
}

// ─── Result<T, E> Simulation ─────────────────────────────────────────────────

// Result[T, E] is the sum type T + E (success or failure).
// Go's idiomatic (T, error) is a structural version of this.
type Result[T, E any] struct {
	value T
	err   E
	isOk  bool
}

func Ok[T, E any](v T) Result[T, E]  { return Result[T, E]{value: v, isOk: true} }
func Err[T, E any](e E) Result[T, E] { return Result[T, E]{err: e} }

func (r Result[T, E]) Match(onOk func(T), onErr func(E)) {
	if r.isOk {
		onOk(r.value)
	} else {
		onErr(r.err)
	}
}

// ─── Church Encoding ─────────────────────────────────────────────────────────

// Church booleans: a boolean is a function that chooses between two options.
type ChurchBool[T any] func(ifTrue, ifFalse T) T

func ChurchTrue[T any]() ChurchBool[T]  { return func(t, _ T) T { return t } }
func ChurchFalse[T any]() ChurchBool[T] { return func(_, f T) T { return f } }

// ChurchNot: flip the branches
func ChurchNot[T any](b ChurchBool[T]) ChurchBool[T] {
	return func(t, f T) T { return b(f, t) }
}

// ─── Never Type ───────────────────────────────────────────────────────────────

// Go has no built-in never type. The convention is a function that
// always panics, documented as "no return."
// In the type system, Go uses empty interfaces for the absurd pattern.

func absurd(msg string) {
	panic(fmt.Sprintf("impossible: %s", msg))
}

// ─── Demo ─────────────────────────────────────────────────────────────────────

func main() {
	// Product types
	p := Point{3, 4}
	fmt.Printf("Point: %+v\n", p)

	// Sum types via interface
	shapes := []Shape{Circle{5}, Rectangle{3, 4}, Triangle{6, 8}}
	for _, s := range shapes {
		fmt.Println(describe(s))
	}

	// Option
	findUser := func(id int) Option[string] {
		if id == 42 {
			return Some("alice")
		}
		return None[string]()
	}

	for _, id := range []int{42, 99} {
		findUser(id).Match(
			func(name string) { fmt.Printf("Found user: %s\n", name) },
			func() { fmt.Printf("User %d not found\n", id) },
		)
	}

	// Church encoding
	t := ChurchTrue[string]()
	f := ChurchFalse[string]()
	fmt.Println(t("yes", "no"))              // yes
	fmt.Println(f("yes", "no"))              // no
	fmt.Println(ChurchNot(t)("yes", "no"))   // no
}
```

### Go-specific considerations

Go's sealed interface pattern approximates sum types but has critical limitations:

- **No exhaustiveness checking**: The compiler never warns about missing cases in a type switch. Adding a new variant to a "sum type" does not produce compile errors in switch statements. You must rely on linting tools (`exhaustive` linter) or the `default: panic(...)` convention.
- **Interface overhead**: Every value stored in an interface carries a type pointer and a data pointer — two words. For small values like `Option<bool>`, this can be worse than just using `(bool, bool)`.
- **Generic workaround for Option**: The `Option[T]` and `Result[T, E]` generics above are clean Go 1.18+, but the standard library does not provide them. The Go idiom remains `(T, bool)` for optional values and `(T, error)` for fallible operations.

## Implementation: Rust

```rust
// Rust has native sum types (enum with data).
// This demonstrates: exhaustiveness checking, the never type,
// Option/Result as regular ADTs, and Church encoding.

// ─── Product Types ────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Copy)]
struct Point {
    x: f64,
    y: f64,
}

// Unit type: () — zero fields, exactly one inhabitant
fn unit_example() -> () {
    // implicitly returns ()
}

// ─── Sum Types ────────────────────────────────────────────────────────────────

// Shape is a sum type. The compiler knows ALL variants at compile time.
#[derive(Debug)]
enum Shape {
    Circle { radius: f64 },
    Rectangle { width: f64, height: f64 },
    Triangle { base: f64, height: f64 },
}

impl Shape {
    fn area(&self) -> f64 {
        match self {
            Shape::Circle { radius }       => std::f64::consts::PI * radius * radius,
            Shape::Rectangle { width, height } => width * height,
            Shape::Triangle { base, height }   => 0.5 * base * height,
            // Uncommenting a new variant like Shape::Pentagon { .. }
            // would produce a compile error here: "pattern `Pentagon` not covered"
        }
    }

    fn describe(&self) -> String {
        match self {
            Shape::Circle { radius } =>
                format!("circle r={radius:.2}, area={:.2}", self.area()),
            Shape::Rectangle { width, height } =>
                format!("rectangle {width}x{height}, area={:.2}", self.area()),
            Shape::Triangle { base, height } =>
                format!("triangle b={base} h={height}, area={:.2}", self.area()),
        }
    }
}

// ─── The Never Type (!) ───────────────────────────────────────────────────────

// ! is the type with no inhabitants.
// A function returning ! never returns normally.
fn diverge() -> ! {
    panic!("I never return")
}

// The never type is a subtype of every type — it can appear anywhere:
fn safe_divide(a: i32, b: i32) -> i32 {
    if b == 0 {
        // panic!() has type !, which is a subtype of i32,
        // so this branch is accepted by the type checker.
        panic!("division by zero")
    } else {
        a / b
    }
}

// ─── Option and Result ARE ADTs ───────────────────────────────────────────────

// Option<T> is defined in std as:
//   enum Option<T> { Some(T), None }
// There is nothing magical about it. We could define our own:

#[derive(Debug)]
enum MyOption<T> {
    MySome(T),
    MyNone,
}

impl<T: std::fmt::Debug> MyOption<T> {
    // map: applies f to the contained value if Some
    fn map<U, F: FnOnce(T) -> U>(self, f: F) -> MyOption<U> {
        match self {
            MyOption::MySome(v) => MyOption::MySome(f(v)),
            MyOption::MyNone   => MyOption::MyNone,
        }
    }

    // and_then: monadic bind — f can itself return MyOption
    fn and_then<U, F: FnOnce(T) -> MyOption<U>>(self, f: F) -> MyOption<U> {
        match self {
            MyOption::MySome(v) => f(v),
            MyOption::MyNone   => MyOption::MyNone,
        }
    }
}

// ─── Church Encoding ─────────────────────────────────────────────────────────

// A Church-encoded boolean is a function that chooses between two branches.
// In Rust, we must use trait objects or closures.

fn church_true<T>(t: T, _f: T) -> T { t }
fn church_false<T>(_t: T, f: T) -> T { f }

// Church-encoded Option: a value is either Some(x) (call on_some with x)
// or None (call on_none). This IS the visitor pattern, formalized.
fn church_option<T, R>(
    value: Option<T>,
    on_some: impl FnOnce(T) -> R,
    on_none: impl FnOnce() -> R,
) -> R {
    match value {
        Some(v) => on_some(v),
        None    => on_none(),
    }
}

// ─── Impossible States Made Unrepresentable ───────────────────────────────────

// Without ADTs: a network request state with a `bool` flag is error-prone.
#[derive(Debug)]
struct BadRequestState {
    is_loading: bool,
    data: Option<String>,
    error: Option<String>,
    // Nothing prevents: is_loading=false, data=None, error=None (illegal!)
    // Or:               is_loading=true,  data=Some(...) (illegal!)
}

// With ADTs: every state is explicitly represented. Illegal states
// are not just checked — they cannot be constructed.
#[derive(Debug)]
enum RequestState {
    Loading,
    Success(String),
    Failed(String),
    // There is no way to construct a state that is simultaneously
    // "loading" and "has data". The compiler prevents it.
}

fn handle_state(state: &RequestState) -> &str {
    match state {
        RequestState::Loading      => "loading...",
        RequestState::Success(s)   => s,
        RequestState::Failed(e)    => e,
        // If we add RequestState::Cancelled, the compiler tells us
        // about every match that needs updating.
    }
}

fn main() {
    let p = Point { x: 3.0, y: 4.0 };
    println!("{p:?}");

    let shapes = [
        Shape::Circle { radius: 5.0 },
        Shape::Rectangle { width: 3.0, height: 4.0 },
        Shape::Triangle { base: 6.0, height: 8.0 },
    ];
    for s in &shapes {
        println!("{}", s.describe());
    }

    // Option chaining with and_then (monadic bind)
    let result = MyOption::MySome(42)
        .map(|x| x * 2)
        .and_then(|x| if x > 50 { MyOption::MySome(x) } else { MyOption::MyNone });
    println!("{result:?}");

    // Church booleans
    println!("{}", church_true("yes", "no"));   // yes
    println!("{}", church_false("yes", "no"));  // no

    // Church-encoded option
    let greeting = church_option(
        Some("Alice"),
        |name| format!("Hello, {name}"),
        || "Hello, stranger".to_string(),
    );
    println!("{greeting}");

    // ADT state machine — impossible states prevented at compile time
    let states = [
        RequestState::Loading,
        RequestState::Success("data loaded".into()),
        RequestState::Failed("connection refused".into()),
    ];
    for s in &states {
        println!("{}", handle_state(s));
    }

    let _ = unit_example();
    println!("safe_divide(10, 2) = {}", safe_divide(10, 2));
}
```

### Rust-specific considerations

- **Exhaustiveness is a core guarantee**: Rust's `match` is exhaustive by contract. The compiler rejects non-exhaustive matches. Adding a variant to an enum produces a compile error at every incomplete `match`. This is the primary safety benefit — it makes illegal states visible at refactoring time.
- **Never type as bottom**: `!` is Rust's bottom type — it is a subtype of every type. `panic!()`, `todo!()`, `unreachable!()`, and `exit()` all have type `!`. This lets them appear in any branch of a `match` without type conflicts.
- **Zero-cost enums**: Rust enums with data do not heap-allocate. A `Result<Vec<u8>, String>` on the stack stores either a `Vec<u8>` or a `String` inline, with a discriminant tag. The compiler may optimize away the tag entirely if it can prove which variant is present.
- **Non-exhaustive annotation**: The `#[non_exhaustive]` attribute marks a public enum as open — callers must include a wildcard branch. Used in public APIs to allow adding variants without a semver break.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Product types | `struct` | `struct` |
| Sum types | `interface` + unexported method (unsealed) | `enum` with variants (sealed) |
| Unit type | `struct{}` | `()` |
| Never type | No equivalent (convention: `panic()`) | `!` — first-class type |
| Exhaustiveness checking | None (use `exhaustive` linter) | Compiler enforces — adding a variant breaks unhandled matches |
| Pattern matching | `type switch` (no binding in case) | `match` (full destructuring, guards, `@` binding) |
| Option equivalent | `(T, bool)` or third-party `Option[T]` | `std::option::Option<T>` — stdlib ADT |
| Result equivalent | `(T, error)` | `std::result::Result<T, E>` — stdlib ADT |
| Illegal state prevention | Convention + documentation | Enforced by type system |

## Production Applications

- **`serde`**: Rust's `#[derive(Deserialize)]` generates match expressions that handle all JSON value variants (`null`, `bool`, `number`, `string`, `array`, `object`). Exhaustiveness ensures no JSON type goes unhandled — a missing case is a compile error.
- **Parser combinators**: `nom` and `pest` represent parse results as `Result<(Input, Output), Error>`. The `?` operator's short-circuiting on `Err` is ADT pattern matching under the hood.
- **State machines**: The `RequestState` example above is idiomatic for async state machines in `tokio`. Network clients modeled as enums guarantee that you cannot accidentally use a closed connection — there is no "connected" state to misread.
- **TypeScript discriminated unions**: TypeScript's `{ kind: 'circle', radius: number } | { kind: 'rect', width: number, height: number }` is a manually-tagged sum type. The TypeScript compiler added exhaustiveness checking (`never` in the default branch) precisely because this pattern is so useful.
- **Go error handling conventions**: The `(T, error)` pattern is Go's structural sum type. The convention that you must check `err != nil` before using `T` is an informal exhaustiveness rule. ADTs formalize this convention into a compiler guarantee.

## Complexity Analysis

**State space explosion**: Product types multiply state spaces. A struct with 10 boolean fields has 2¹⁰ = 1024 possible states. If only 10 of those states are valid, you have 1014 illegal states that your code must defend against at runtime. Sum types that model only valid states reduce the state space to the 10 valid combinations — no runtime defense needed.

**Match performance**: Rust's `match` compiles to jump tables or cascaded branches, whichever is faster. The compiler optimizes enum discriminants. An `Option<i32>` on x86-64 is often 8 bytes: 4 for the discriminant, 4 for the `i32`. With niche optimization, `Option<NonZeroI32>` is also 4 bytes (using the value 0 as the `None` discriminant).

**Compilation time**: Large enums with many variants that derive common traits (`Clone`, `Debug`, `PartialEq`) generate substantial amounts of match code. If a widely-used enum changes, all crates that depend on it must recompile. Consider `#[non_exhaustive]` for public enums that may grow.

## Common Pitfalls

1. **God structs instead of sum types**: A struct with five optional fields where only certain combinations are valid is a hidden sum type with illegal states. Refactor to an enum where each variant carries only the fields that are valid for that state.

2. **`default` in Go type switch swallows new variants**: The `default: return "unknown"` pattern in a Go type switch silently handles variants you forgot. Use `default: panic(fmt.Sprintf("unhandled shape: %T", s))` to catch missing cases at runtime, and the `exhaustive` linter to catch them at compile time.

3. **Wrapping errors loses the sum type**: In Go, returning `fmt.Errorf("wrapped: %w", specificErr)` converts a concrete error type into `error`, losing the sum type structure. If callers need to distinguish error kinds, return the concrete type or a typed sentinel.

4. **`Option<Option<T>>` nesting**: A `Some(None)` is different from `None`. When chaining operations that return `Option`, use `and_then` instead of `map` to avoid double-wrapping. `option.map(f)` where `f` returns `Option<U>` gives `Option<Option<U>>` — probably not what you want.

5. **Church encoding in production**: It is illuminating but not idiomatic in Go or Rust. Do not replace Rust `match` with Church-encoded closures. The visitor pattern in Go is fine for its intended use cases (double dispatch, external operations on sealed types), but not as a replacement for idiomatic pattern matching.

## Exercises

**Exercise 1** (30 min): Implement a `Tree<T>` ADT in both Go (interface-based) and Rust (enum-based): `Leaf(T)` or `Branch(Tree<T>, Tree<T>)`. Write `depth()`, `count()`, and `map(f)`. Compare how exhaustiveness enforcement differs between the two implementations.

**Exercise 2** (2–4 h): Model a vending machine state as an ADT in Rust. States: `Idle`, `HasMoney(u32)`, `Dispensing(Product)`. Transitions: `insert_coin`, `select_product`, `take_product`. Enforce that `select_product` is only callable in `HasMoney` and `take_product` only in `Dispensing` — using the type system, not runtime checks. This is the "typestate" pattern.

**Exercise 3** (4–8 h): Implement a JSON value ADT in both Go and Rust:
- Variants: `Null`, `Bool(bool)`, `Number(f64)`, `Str(String)`, `Array(Vec<JsonValue>)`, `Object(HashMap<String, JsonValue>)`
- Implement `pretty_print(indent: usize)` and `get_path(&[&str]) -> Option<&JsonValue>` (for JSON path traversal like `obj["users"][0]["name"]`)
- Compare exhaustiveness behavior when you add a `BigInt(i128)` variant

**Exercise 4** (8–15 h): Implement a type-safe SQL query builder using ADTs. The sum type should represent: `SELECT`, `INSERT`, `UPDATE`, `DELETE`, `CREATE TABLE`. Each variant carries the parts valid for that statement (table name, columns, WHERE clause, values). Enforce at compile time that `WHERE` clauses are not added to `INSERT`. The type system should prevent constructing a syntactically invalid SQL statement.

## Further Reading

### Foundational Papers
- Milner, R. (1984). "A proposal for Standard ML." *LFP 1984*. — ML introduced algebraic data types to mainstream languages. Section 3 defines types.
- Wadler, P. (1987). "Views: A way for pattern matching to cohabit with data abstraction." *POPL 1987*. — How to reconcile ADT exhaustiveness with information hiding.
- Chlipala, A. (2022). *Certified Programming with Dependent Types*. — Free online. Chapter 2 covers inductive types (the dependent-type generalization of ADTs).

### Books
- *Types and Programming Languages* (Pierce) — Chapter 11 (simple extensions), Chapter 13 (references), Chapter 20 (recursive types). ADTs as recursive types are how `List` and `Tree` are formalized.
- *Programming with Types* (Granin) — Practical ADT patterns in TypeScript, directly applicable to Go/Rust thinking.

### Production Code to Read
- Rust standard library: `core/src/option.rs` and `core/src/result.rs` — the entire source of `Option` and `Result`. Each is a short enum. The extensive method implementations show what you can build on an ADT.
- `serde_json/src/value.rs` — the `Value` enum for JSON. Study how `Index` and `get` are implemented without runtime type errors.

### Talks
- "Making Impossible States Impossible" (Richard Feldman, Elm Conf 2016) — the clearest explanation of using ADTs to eliminate illegal states. The Elm examples translate directly to Rust.
- "The Power of Algebraic Data Types" (Edwin Brady, Lambda Days 2019) — the Idris creator shows how dependently-typed ADTs eliminate entire classes of runtime errors.
