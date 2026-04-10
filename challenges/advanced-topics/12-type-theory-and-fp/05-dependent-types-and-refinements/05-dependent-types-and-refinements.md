<!--
type: reference
difficulty: insane
section: [12-type-theory-and-fp]
concepts: [dependent-types, refinement-types, const-generics, liquid-haskell, kani, typenum, pi-types, sigma-types, indexed-types, proof-carrying-code]
languages: [go, rust]
estimated_reading_time: 85 min
bloom_level: evaluate
prerequisites: [algebraic-data-types, higher-kinded-types, rust-const-generics]
papers: [Xi-Pfenning-1999-dependent-types-practical, Rondon-2008-liquid-types, Barrett-2011-smt-lib]
industry_use: [Rust-const-generics, nalgebra, ndarray, Kani-AWS, Liquid-Haskell-financial-systems, Idris-2-systems]
language_contrast: high
-->

# Dependent Types and Refinements

> A dependent type is a type that carries a value. A refinement type is a type that carries a proof. Together, they let the compiler verify properties that normally live in runtime assertions.

## Mental Model

A normal generic function like `fn first<T>(v: &[T]) -> &T` has a problem: it panics if the slice is empty. The type signature says nothing about emptiness — the contract is invisible. You find out at runtime.

A dependent type can encode the length of the slice in the type: `fn first<T, const N: usize>(v: &[T; N]) -> &T` where the constraint `N > 0` is part of the type. Now `first` cannot even be called with an empty array — the compiler rejects it.

This is the core idea of dependent types: types can depend on values. The canonical example is a vector type where the length is a type parameter: `Vec<T, N>` where `N : Nat`. `append` on a `Vec<T, 3>` and a `Vec<T, 4>` produces a `Vec<T, 7>`. The length arithmetic happens at compile time. Getting it wrong is a type error.

Refinement types are a restricted but practical version: instead of full dependent types (Agda, Coq, Idris), you annotate types with logical predicates. `{x: Int | x > 0}` is the type of positive integers. The type checker uses an SMT solver (Z3, CVC4) to prove that your code always produces positive integers where that type is required. Liquid Haskell implements this; `Kani` does bounded model checking for Rust.

**Why this matters for production code:**
- Rust's `const generics` give you limited dependent types for integer dimensions: `[T; N]`, `Matrix<f64, ROWS, COLS>`, `[u8; 32]` for SHA-256 digests
- Rust's newtype pattern with smart constructors is a manual form of refinement typing: `NonZeroU32`, `NonEmpty<T>`, `BoundedU8<MIN, MAX>`
- Kani can verify that your Rust code is free of integer overflows, out-of-bounds accesses, and assertion violations for bounded inputs
- Understanding these patterns makes you write APIs that cannot be misused — preconditions enforced by the type system, not documentation

## Core Concepts

### Pi Types (Dependent Function Types)

In a dependently-typed system, `Π(n : Nat). Vec<T, n>` is a function type where the *return type depends on the argument value*. Writing `Π(n : Nat). Vec<T, n>` means: for any natural number `n`, produce a vector of length `n`.

In Rust, const generics give us limited pi types: `fn zeroes<const N: usize>() -> [i32; N]`. The return type `[i32; N]` depends on the type-level value `N`. The limitation: `N` must be known at compile time (it is a const expression, not a runtime value).

### Sigma Types (Dependent Pair Types)

`Σ(n : Nat). Vec<T, n>` is a pair where the *type of the second component depends on the value of the first*. This is how you represent "a vector along with its length proof."

In Rust, this is approximated by newtypes that carry invariants:
```rust
struct NonEmpty<T>(Vec<T>); // invariant: inner Vec always has len >= 1
```

The invariant is enforced by the smart constructor, not the type system directly.

### Refinement Types

A refinement type `{x : T | P(x)}` is a type T narrowed by predicate P. Values of this type are guaranteed to satisfy P. The type checker must prove that all computations producing this type always satisfy P — it delegates this proof to an SMT solver.

Liquid Haskell notation:
```haskell
{-@ divide :: Int -> {v: Int | v /= 0} -> Int @-}
divide :: Int -> Int -> Int
divide x y = x `div` y
```

The annotation says the second argument must be non-zero. Liquid Haskell checks at compile time that every call site provides a non-zero second argument.

### Const Generics as Limited Dependent Types

Rust's `const N: usize` in a generic parameter is a type-level natural number. It is "dependent" in the sense that types depend on its value, but it is restricted: N must be a compile-time constant. There is no runtime computation of N.

This handles the most common use case: fixed-size arrays, matrix dimensions, buffer sizes, digest lengths.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math"
)

// ─── Smart Constructors as Manual Refinement Types ────────────────────────────
//
// Go has no dependent types or refinement types.
// The closest equivalent: newtypes with constructors that enforce invariants.
// The invariant lives in the constructor, not the type.

// PositiveInt is a refinement of int: only positive values.
// The invariant is enforced at construction time, not by the compiler.
type PositiveInt struct{ n int }

func NewPositiveInt(n int) (PositiveInt, error) {
	if n <= 0 {
		return PositiveInt{}, fmt.Errorf("NewPositiveInt: %d is not positive", n)
	}
	return PositiveInt{n}, nil
}

func (p PositiveInt) Value() int { return p.n }

// BoundedInt[min, max] is a refinement of int: value in [min, max].
// Simulating dependent types via generic type parameters.
type BoundedInt[Min, Max interface{ ~int }] struct {
	n int
}

// NewBoundedInt is a smart constructor — the bounds are documented but not
// type-checked. This is the limitation of Go's approach.
func NewBoundedInt[Min, Max int](n int, min, max int) (BoundedInt[Min, Max], error) {
	if n < min || n > max {
		return BoundedInt[Min, Max]{}, fmt.Errorf("%d not in [%d, %d]", n, min, max)
	}
	return BoundedInt[Min, Max]{n}, nil
}

// ─── Compile-Time Size Enforcement via Type Tags ───────────────────────────────
//
// Go's arrays have compile-time sizes: [32]byte is a distinct type from [64]byte.
// This is the limited form of dependent types Go provides.

type SHA256Digest [32]byte
type SHA512Digest [64]byte

// These are separate types — you cannot accidentally pass a SHA512 where SHA256
// is expected. The size is encoded in the type, enforced at compile time.

func newSHA256(data []byte) SHA256Digest {
	// In production: sha256.Sum256(data) — this is illustrative
	var result SHA256Digest
	for i, b := range data {
		if i >= 32 {
			break
		}
		result[i] = b
	}
	return result
}

// ─── Typestate: Values as State Machine Participants ──────────────────────────
//
// Typestate uses the type system to encode state machine validity.
// This is a form of dependent typing where the type depends on the program state.

type ConnectionState interface{ state() }
type Disconnected struct{}
type Connected struct{ host string }
type Closed struct{}

func (Disconnected) state() {}
func (Connected) state()    {}
func (Closed) state()       {}

// Each transition function accepts only the right state:
func Connect(d Disconnected, host string) (Connected, error) {
	if host == "" {
		return Connected{}, fmt.Errorf("empty host")
	}
	fmt.Printf("Connected to %s\n", host)
	return Connected{host}, nil
}

func Disconnect(c Connected) Closed {
	fmt.Printf("Closed connection to %s\n", c.host)
	return Closed{}
}

// Send only works on Connected — Disconnected.Send does not exist.
// This is compile-time enforcement of protocol order.
func (c Connected) Send(data string) {
	fmt.Printf("Sending '%s' to %s\n", data, c.host)
}

// ─── NonEmpty: A Refinement on Slices ─────────────────────────────────────────

// NonEmpty[T] is the refinement of []T where len >= 1.
// The first element is always safe to access.
type NonEmpty[T any] struct {
	head T
	tail []T
}

func NewNonEmpty[T any](first T, rest ...T) NonEmpty[T] {
	return NonEmpty[T]{head: first, tail: rest}
}

func FromSlice[T any](s []T) (NonEmpty[T], error) {
	if len(s) == 0 {
		return NonEmpty[T]{}, fmt.Errorf("empty slice")
	}
	return NonEmpty[T]{head: s[0], tail: s[1:]}, nil
}

// Head is total (always safe) — no runtime panic possible.
func (n NonEmpty[T]) Head() T { return n.head }

// Tail is safe — may be empty, but that is represented correctly.
func (n NonEmpty[T]) Tail() []T { return n.tail }

// Map over NonEmpty — result is still NonEmpty (structure preserved).
func MapNonEmpty[A, B any](n NonEmpty[A], f func(A) B) NonEmpty[B] {
	tail := make([]B, len(n.tail))
	for i, v := range n.tail {
		tail[i] = f(v)
	}
	return NonEmpty[B]{head: f(n.head), tail: tail}
}

func main() {
	// PositiveInt
	if p, err := NewPositiveInt(42); err == nil {
		fmt.Printf("PositiveInt: %d\n", p.Value())
	}
	_, err := NewPositiveInt(-5)
	fmt.Printf("NewPositiveInt(-5): %v\n", err)

	// SHA256 vs SHA512 — different types, cannot be confused
	digest := newSHA256([]byte("hello"))
	fmt.Printf("SHA256 digest type: [32]byte, first byte: %d\n", digest[0])

	// Typestate connection
	var d Disconnected
	conn, err := Connect(d, "localhost:8080")
	if err == nil {
		conn.Send("ping")
		_ = Disconnect(conn)
		// conn.Send("ping") -- would compile but conn is now "logically" invalid
		// In full typestate (Rust affine types), this would be a compile error
	}

	// NonEmpty
	ne := NewNonEmpty(1, 2, 3, 4, 5)
	fmt.Printf("Head: %d, Tail: %v\n", ne.Head(), ne.Tail())
	doubled := MapNonEmpty(ne, func(x int) int { return x * 2 })
	fmt.Printf("Doubled head: %d\n", doubled.Head())

	// From empty slice — safe error
	_, err = FromSlice[int]([]int{})
	fmt.Printf("FromSlice([]): %v\n", err)

	_ = math.Pi // suppress unused import
}
```

### Go-specific considerations

Go provides compile-time size constraints via fixed-size arrays (`[32]byte`, `[64]byte`) and no further. There are no const generics, no refinement type annotations, and no built-in way to express "positive integer" in the type system beyond newtypes with smart constructors.

The typestate pattern (using distinct types for each state, with transition functions that consume the old state) is idiomatic Go for protocol safety, but it is advisory — the compiler cannot enforce that you do not hold onto an "old" state after a transition, unlike Rust's move semantics.

## Implementation: Rust

```rust
// Rust provides:
// - Const generics: type-level integers (limited dependent types)
// - Newtypes with smart constructors (manual refinement types)
// - Kani: bounded model checker (external verification)
// - typenum: type-level arithmetic (full Peano arithmetic)

// ─── Const Generics as Dependent Types ───────────────────────────────────────

use std::ops::Add;

// A fixed-size ring buffer — size is part of the type.
// You cannot create a RingBuffer<T, 0>; it is a type error to call certain ops.
#[derive(Debug)]
struct RingBuffer<T, const CAP: usize> {
    data:  [Option<T>; CAP],
    head:  usize,
    count: usize,
}

impl<T: Copy + Default, const CAP: usize> RingBuffer<T, CAP> {
    // Compile-time assertion: CAP must be > 0.
    // This is evaluated when the type is instantiated.
    const _NON_ZERO: () = assert!(CAP > 0, "RingBuffer capacity must be > 0");

    fn new() -> Self {
        let _ = Self::_NON_ZERO;
        RingBuffer {
            data:  [None; CAP],
            head:  0,
            count: 0,
        }
    }

    fn push(&mut self, val: T) -> bool {
        if self.count == CAP {
            return false; // full
        }
        let idx = (self.head + self.count) % CAP;
        self.data[idx] = Some(val);
        self.count += 1;
        true
    }

    fn pop(&mut self) -> Option<T> {
        if self.count == 0 {
            return None;
        }
        let val = self.data[self.head].take();
        self.head = (self.head + 1) % CAP;
        self.count -= 1;
        val
    }

    fn is_full(&self) -> bool { self.count == CAP }
}

// Concatenating two arrays — length is N + M at the type level.
// generic_const_exprs is needed for N + M; here we use a workaround.
fn concat_arrays<T: Copy + Default, const N: usize, const M: usize, const NM: usize>(
    a: [T; N],
    b: [T; M],
) -> [T; NM] {
    // Compile-time check that NM == N + M
    assert_eq!(NM, N + M, "NM must equal N + M");
    let mut result = [T::default(); NM];
    result[..N].copy_from_slice(&a);
    result[N..N + M].copy_from_slice(&b);
    result
}

// ─── Refinement Types via Newtypes ────────────────────────────────────────────

// NonZeroPositive: a positive, non-zero integer.
// The invariant is in the constructor — the compiler cannot verify it,
// but calling code is forced to handle the error.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
struct Positive(u32); // private inner field — cannot be constructed except via new()

impl Positive {
    pub fn new(n: u32) -> Option<Self> {
        if n > 0 { Some(Positive(n)) } else { None }
    }

    pub fn get(self) -> u32 { self.0 }
}

impl Add for Positive {
    type Output = Positive;
    // Addition of two positives is always positive — total, no error possible
    fn add(self, rhs: Positive) -> Positive {
        Positive(self.0 + rhs.0) // safe: u32 overflow aside
    }
}

// NonEmpty<T>: a vector that is guaranteed to have at least one element.
#[derive(Debug, Clone)]
struct NonEmpty<T> {
    head: T,
    tail: Vec<T>,
}

impl<T> NonEmpty<T> {
    pub fn new(head: T) -> Self {
        NonEmpty { head, tail: Vec::new() }
    }

    pub fn from_vec(mut v: Vec<T>) -> Option<Self> {
        if v.is_empty() {
            return None;
        }
        v.reverse();
        let head = v.pop().unwrap();
        v.reverse();
        Some(NonEmpty { head, tail: v })
    }

    // head() is total — never panics because the invariant guarantees len >= 1
    pub fn head(&self) -> &T { &self.head }

    pub fn len(&self) -> usize { 1 + self.tail.len() }

    pub fn map<U, F: FnMut(T) -> U>(self, mut f: F) -> NonEmpty<U> {
        NonEmpty {
            head: f(self.head),
            tail: self.tail.into_iter().map(f).collect(),
        }
    }
}

// ─── Typestate Pattern with Move Semantics ────────────────────────────────────
// Rust's ownership enforces typestate: once a state is consumed (moved),
// you cannot use the old state value — the compiler prevents it.

struct Disconnected;
struct Connected { host: String }
struct Closed;

impl Disconnected {
    fn connect(self, host: &str) -> Result<Connected, String> {
        if host.is_empty() {
            return Err("empty host".into());
        }
        println!("Connected to {host}");
        Ok(Connected { host: host.to_string() })
    }
}

impl Connected {
    fn send(&self, data: &str) {
        println!("Sending '{}' to {}", data, self.host);
    }

    fn disconnect(self) -> Closed {
        println!("Closed connection to {}", self.host);
        Closed
    }
}

// Closed has no methods — nothing you can do with a closed connection.
// The compiler enforces this: after disconnect(), the Connected value is moved.

// ─── Kani: Bounded Model Checking (documentation example) ─────────────────────
// Kani verifies correctness properties for bounded inputs.
// This code shows what Kani annotations look like — run with `cargo kani`.

// #[cfg(kani)]
// mod verification {
//     use super::*;
//
//     #[kani::proof]
//     fn verify_positive_add() {
//         let a: u32 = kani::any();
//         let b: u32 = kani::any();
//         kani::assume(a > 0 && b > 0);
//         kani::assume(a < u32::MAX / 2 && b < u32::MAX / 2); // prevent overflow
//
//         let pa = Positive::new(a).unwrap();
//         let pb = Positive::new(b).unwrap();
//         let sum = pa + pb;
//
//         // Kani verifies this holds for ALL inputs satisfying the assumptions
//         assert!(sum.get() > 0, "sum of positives must be positive");
//         assert!(sum.get() == a + b, "addition must be correct");
//     }
//
//     #[kani::proof]
//     fn verify_ring_buffer_push_pop() {
//         let mut buf: RingBuffer<u32, 4> = RingBuffer::new();
//         let val: u32 = kani::any();
//         assert!(buf.push(val));
//         let popped = buf.pop();
//         assert_eq!(popped, Some(val));
//     }
// }

// ─── Const-Generic Digest Types ───────────────────────────────────────────────
// Types that encode their security parameters — cannot accidentally mix them.

#[derive(Debug, Clone, PartialEq, Eq)]
struct Digest<const N: usize>([u8; N]);

type Sha256Digest = Digest<32>;
type Sha512Digest = Digest<64>;

// fake_hash is illustrative — just zeroes
fn fake_hash<const N: usize>(input: &[u8]) -> Digest<N> {
    let mut bytes = [0u8; N];
    for (i, &b) in input.iter().take(N).enumerate() {
        bytes[i] = b;
    }
    Digest(bytes)
}

fn main() {
    // Const-generic ring buffer
    let mut buf: RingBuffer<i32, 4> = RingBuffer::new();
    for i in 0..4 {
        buf.push(i);
    }
    println!("Buffer full: {}", buf.is_full());
    while let Some(v) = buf.pop() {
        print!("{v} ");
    }
    println!();

    // Positive newtype
    let a = Positive::new(3).unwrap();
    let b = Positive::new(4).unwrap();
    println!("Positive add: {}", (a + b).get());

    let bad = Positive::new(0);
    println!("Positive::new(0): {bad:?}");

    // NonEmpty
    let ne = NonEmpty::from_vec(vec![1, 2, 3]).unwrap();
    println!("NonEmpty head: {}, len: {}", ne.head(), ne.len());

    let doubled = ne.map(|x| x * 2);
    println!("Doubled: {} ...", doubled.head());

    // Typestate with move semantics
    let d = Disconnected;
    if let Ok(conn) = d.connect("localhost:9000") {
        conn.send("hello");
        let _closed = conn.disconnect();
        // conn.send("late message"); // COMPILE ERROR: value moved here
    }

    // Digest types
    let sha256: Sha256Digest = fake_hash(b"hello");
    let sha512: Sha512Digest = fake_hash(b"hello");
    println!("SHA256 len: {}", sha256.0.len());
    println!("SHA512 len: {}", sha512.0.len());
    // Cannot pass sha256 where sha512 is expected — they are different types.
}
```

### Rust-specific considerations

- **Const generic restrictions**: Const generics work for integers, booleans, and chars. You cannot use `f32` or custom types as const parameters. Const expressions (`N + M`, `N * M`) require `#![feature(generic_const_exprs)]` (unstable as of 2025). Workarounds: use `typenum` for arithmetic, or add a third const parameter and assert equality.
- **`const _: () = assert!(...)`**: Compile-time assertions using associated constants. This is the Rust equivalent of a dependent type constraint — it is checked when the type is instantiated, not when the code is run.
- **Kani** (AWS): A bounded model checker for Rust. It verifies Rust programs against safety properties (no panics, no overflows, no out-of-bounds) for bounded input ranges. Run with `cargo kani`. Unlike fuzzing, it exhaustively explores all inputs within the bound.
- **`typenum` crate**: Type-level integers as associated types. `typenum::U5 : typenum::Unsigned`. Arithmetic at type level: `typenum::op!(U3 + U4 == U7)`. Used by `nalgebra`, `generic-array`. More expressive than const generics but produces complex error messages.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Compile-time integer in type | Fixed-size arrays `[N]T` — N must be literal | Const generics `const N: usize` |
| Refinement types | None — smart constructors + runtime checks | None native — newtypes + Kani for verification |
| Typestate | Distinct types for states; Move not enforced | Distinct types + move semantics enforces single use |
| Dependent type equivalent | None | Const generics (limited); `typenum` (full arithmetic) |
| Model checking | No built-in; external tools | Kani (`cargo kani`); MIRI for UB |
| Proof assistant integration | No | Kani; lean4-style proofs experimental |

## Production Applications

- **`nalgebra`**: `Matrix<f64, U3, U4>` — matrix dimensions from `typenum`. Matrix multiplication checks dimensions at compile time. Used in robotics (ROS2) and graphics (bevy).
- **`generic-array`**: `GenericArray<u8, U32>` — fixed-size arrays with `typenum` lengths. Used by `digest` crate (SHA-256, BLAKE3) to encode digest sizes in the type.
- **AWS Kani verification**: Amazon uses Kani to verify Rust code in their firecracker VMM and s2n-tls library. Proofs verify bounds-safety and integer overflow freedom for security-critical paths.
- **Liquid Haskell in financial systems**: Banks use Liquid Haskell to verify that financial arithmetic is within bounds and that invariants (non-negative balances, non-zero divisors) hold statically.
- **Go's `crypto/sha256`**: The `Sum256(data []byte) [32]byte` signature returns a fixed-size array, not a slice. The 32-byte constraint is in the type. This prevents the caller from accidentally truncating the digest.

## Complexity Analysis

**Const generic monomorphization**: Each unique value of a const parameter produces a separate compiled function. `RingBuffer<T, 4>` and `RingBuffer<T, 8>` are different types with different generated code. For a library with many buffer sizes, this can cause binary size growth. Profile with `cargo bloat`.

**`typenum` compile time**: Type-level arithmetic via `typenum` performs computation at compile time by traversing type-level linked lists. Operations on large numbers (e.g., `U1000`) can be slow to compile. Prefer const generics when the arithmetic is simple; use `typenum` only when you need full arithmetic.

**Kani verification scope**: Kani is bounded — it verifies properties for inputs up to a configurable bound, not all possible inputs. Verification time grows exponentially with bound size. Limit verification to security-critical functions, not entire codebase.

## Common Pitfalls

1. **Const generic inference failures**: Rust sometimes cannot infer a const generic parameter and requires explicit specification: `concat_arrays::<i32, 3, 4, 7>(a, b)`. When the constraint `NM == N + M` cannot be inferred, you must provide it explicitly.

2. **Newtype invariants not sealed**: If `Positive(u32)` has a public inner field, any code can write `Positive(0)` and bypass the invariant. Always make the inner field private; expose only smart constructors.

3. **Typestate with shared references**: Rust's typestate works because move semantics consume the state. If you use `Arc<Mutex<State>>`, the typestate pattern breaks — multiple owners can hold the old state. Typestate works cleanest with single-ownership values.

4. **Const generic expressions at function boundaries**: `concat_arrays` needs `NM` as a third parameter because `N + M` is not yet a stable const expression in generic bounds. This is a known limitation that `generic_const_exprs` will eventually solve.

5. **Mistaking runtime validation for compile-time verification**: A smart constructor (`Positive::new(n)`) validates at runtime and returns `Option`. This is not the same as a type-level proof. The type says "this was validated at some point," not "this is always positive by construction." For truly compile-time guarantees, you need const generics or a formal verifier.

## Exercises

**Exercise 1** (30 min): In Rust, create a `BoundedU8<const MIN: u8, const MAX: u8>` newtype that enforces `MIN <= value <= MAX` at construction time. Implement `Add` such that it returns `Result<BoundedU8<MIN, MAX>, String>` to handle potential overflow. Test that `BoundedU8<0, 100>::new(150)` fails and `BoundedU8<0, 100>::new(50) + BoundedU8<0, 100>::new(60)` fails.

**Exercise 2** (2–4 h): Implement a type-safe unit system using const generics or newtypes. Represent physical units at the type level: `Meters(f64)`, `Seconds(f64)`, `MetersPerSecond(f64)`. Implement `Div<Meters, Seconds> -> MetersPerSecond` so that dividing meters by seconds produces a speed. Make it impossible to add meters to seconds — a compile error, not a runtime error.

**Exercise 3** (4–8 h): Build a compile-time-verified state machine in Rust. Model a TCP connection: `Closed`, `Listen`, `SynReceived`, `Established`, `FinWait1`, `FinWait2`, `TimeWait`. Each state is a distinct type. Each valid transition is a function that consumes the old state (moved) and returns the new state. Invalid transitions (e.g., sending data on a `Closed` connection) should not compile. Verify that the happy path compiles and an invalid transition fails at compile time.

**Exercise 4** (8–15 h): Annotate a small cryptographic library (SHA-256 + HMAC) with Kani proofs. Verify: no integer overflows in the compression function, no out-of-bounds array accesses, that the output length is always exactly 32 bytes, and that two different inputs produce different outputs (collision resistance — Kani can only verify this for small bounded inputs). Use `#[kani::proof]` and `kani::any()` for symbolic inputs.

## Further Reading

### Foundational Papers
- Xi, H., & Pfenning, F. (1999). "Dependent types in practical programming." *POPL 1999*. — Introduced DML (Dependent ML), the practical dependent type system that influenced ML family languages.
- Rondon, P. et al. (2008). "Liquid types." *PLDI 2008*. — The paper that introduced Liquid Types (refinement types verified by SMT). The basis for Liquid Haskell.
- Brady, E. (2013). "Idris: A general-purpose dependently typed programming language." *Journal of Functional Programming*, 23(5). — Full dependent types in a practical language.

### Books
- *Certified Programming with Dependent Types* (Chlipala) — Free online. Coq as a proof assistant. Chapter 3 shows how indexed types (like length-indexed vectors) work in a fully dependently-typed system.
- *Software Foundations* (Pierce et al.) — Free online. Volume 1 covers Coq basics; Volume 3 covers verified programs.

### Production Code to Read
- `nalgebra/src/base/matrix.rs` — `Matrix<T, R, C, S>` where R and C are `typenum` dimension types. The implementation of type-safe matrix operations.
- `s2n-tls` AWS Kani proofs — `https://github.com/aws/s2n-tls/tree/main/tests/kani` — Production Kani verification of a TLS implementation.

### Talks
- "Dependent Types in Haskell" (Stephanie Weirich, POPL 2017) — gradual path to dependent types in Haskell. Maps to Rust's gradual approach with const generics.
- "Kani: Verifying Rust Programs in Production" (Celina Val, RustConf 2022) — How AWS uses Kani for security verification.
