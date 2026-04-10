<!--
type: reference
difficulty: insane
section: [12-type-theory-and-fp]
concepts: [higher-kinded-types, type-constructors, GATs, defunctionalization, witness-types, kind-system, type-families, associated-types]
languages: [go, rust]
estimated_reading_time: 80 min
bloom_level: evaluate
prerequisites: [category-theory-for-programmers, algebraic-data-types, rust-traits, go-generics]
papers: [Yallop-White-2014-lightweight-HKT, Chakravarty-2005-associated-type-synonyms, Huet-Coquand-1988-calculus-of-constructions]
industry_use: [Haskell-base, Scala-cats, Rust-GATs-stable, serde-without-HKT]
language_contrast: high
-->

# Higher-Kinded Types

> A type constructor like `Option` is not a type — it is a function from types to types. Higher-kinded types let you abstract over these type-level functions.

## Mental Model

In most type systems, you work with two things: values (runtime data) and types (compile-time descriptions of values). Higher-kinded types add a third level: *kinds*, which are types of types.

Concrete types like `i32`, `String`, and `bool` have kind `*` (pronounced "type" or "star"). They are ready to hold values.

Type constructors like `Option`, `Vec`, and `Result` have kind `* -> *` — they take one type argument and produce a type. `Option<i32>` has kind `*`; `Option` alone has kind `* -> *`. Similarly, `Result` has kind `* -> * -> *` — it takes two type arguments.

The word "higher-kinded" means: abstracting over type constructors, not just over types. Instead of writing "a function that works for any type `T`", you write "a function that works for any type constructor `F`". The canonical example:

```haskell
-- Functor works for any F of kind * -> *
class Functor f where
  fmap :: (a -> b) -> f a -> f b
```

Here `f` ranges over type constructors (`Option`, `Vec`, `Result`'s Ok-side, etc.). Go and standard Rust cannot express this directly — there is no way to say "T is a type constructor of kind * -> *" in a trait bound. This is the HKT gap.

Why does this matter for production code? Without HKT, you cannot write a single `Functor` or `Monad` trait that all container types implement. Every functional pattern must be re-implemented per concrete type. With HKT, you can write code that is parametric over the *container*, not just the *element*: "a pipeline that works whether the container is `Option`, `Vec`, or `Result`."

Rust gets partway there with GATs (Generic Associated Types, stable since 1.65). GATs let you express type constructors in associated types, enabling limited HKT simulation. Go does not have GATs, but there are encoding tricks (defunctionalization, witness types) that simulate the behavior with more ceremony.

## Core Concepts

### Kinds

The kind system is the type system of types. In Haskell notation:
- `*` — a concrete type: `Int`, `Bool`, `String`
- `* -> *` — a unary type constructor: `Maybe`, `[]` (list), `IO`
- `* -> * -> *` — a binary type constructor: `Either`, `Map`
- `(* -> *) -> *` — takes a type constructor and produces a type: used in `Fix` (fixed points of functors)

When you write `fn foo<T: SomeTrait>(x: T)` in Rust, `T` has kind `*`. When you write a trait with a GAT `type Mapped<B>`, that associated type is a type constructor of kind `* -> *`.

### Defunctionalization for HKT in Go

Defunctionalization (Reynolds, 1972) is a technique for compiling higher-order functions to first-order code. Applied to types, it lets you simulate HKT in a language without it.

The idea: instead of abstracting over a type constructor `F<_>`, create a "witness" type that represents `F` and use it to look up the concrete mapping at any `T`. The type system cannot enforce this fully, but the pattern gives you the organization benefits.

### GATs (Generic Associated Types) in Rust

GATs are the key addition that lets Rust traits express HKT-like constraints. An associated type in a trait can itself be generic:

```rust
trait Container {
    type Item;                        // ordinary associated type — fixed type
    type Mapped<B>;                   // GAT — a family of types, one per B
}
```

With `type Mapped<B>`, implementing `Functor` can say "mapping over `Container<A>` with `A -> B` produces `Self::Mapped<B>`". This is the type constructor abstraction that HKT provides.

## Implementation: Go

```go
package main

import "fmt"

// ─── Simulating HKT in Go with Witness Types ─────────────────────────────────
//
// The key insight: we cannot abstract over F<T> directly.
// Instead, we use an "Apply" type as a witness:
//   Apply[F, T] = "apply type constructor F to type argument T"
// This is simulated with a concrete container holding the actual value.

// HKT is the simulation interface.
// Brand is a phantom tag type (never instantiated) that represents F.
// T is the contained type.
type HKT[Brand any, T any] interface {
	// In a real HKT system, this would BE the type F<T>.
	// Here, it is an interface that concrete types implement.
	hktBrand() Brand
}

// ─── Option as an HKT witness ─────────────────────────────────────────────────

// OptionBrand is a phantom type that tags the Option constructor.
type OptionBrand struct{}

type Option[T any] struct {
	value  T
	isSome bool
}

func Some[T any](v T) *Option[T]   { return &Option[T]{v, true} }
func None[T any]() *Option[T]     { return &Option[T]{} }

// Option satisfies HKT[OptionBrand, T]
func (o *Option[T]) hktBrand() OptionBrand { return OptionBrand{} }

// ─── Writing "Mappable" without true HKT ─────────────────────────────────────
//
// We cannot write Functor[F[_]] in Go.
// We CAN write an interface with a Map method specific to a return type.
// The tradeoff: we cannot abstract the return type over the same functor.

// Mappable[A, B] represents a type that can be mapped from A to B.
// We must include both A and B as type parameters — no way to express
// "the output has the same outer structure as the input."
type Mappable[A, B any] interface {
	FMap(func(A) B) *Option[B]
}

// For Option specifically:
func FMapOption[A, B any](opt *Option[A], f func(A) B) *Option[B] {
	if opt.isSome {
		return Some(f(opt.value))
	}
	return None[B]()
}

// ─── Defunctionalization: CPS-based HKT Simulation ───────────────────────────
//
// A more principled simulation using the defunctionalization pattern.
// We represent type constructors as phantom types and use Apply to retrieve
// the concrete type.

// Apply[F, A] is the "application" of type constructor F to type A.
// The trick: a concrete Apply implementation wraps the real value.

type Apply[F, A any] struct {
	val interface{} // holds the actual F<A> value
}

// For each type constructor, we need an "injection" and "projection" function.
// This is the encoding overhead compared to native HKT.

func InjectOption[A any](opt *Option[A]) Apply[OptionBrand, A] {
	return Apply[OptionBrand, A]{val: opt}
}

func ProjectOption[A any](a Apply[OptionBrand, A]) *Option[A] {
	return a.val.(*Option[A])
}

// Now we can write a "generic" map that works for Option via Apply:
func FmapViaApply[A, B any](
	a Apply[OptionBrand, A],
	f func(A) B,
) Apply[OptionBrand, B] {
	opt := ProjectOption(a)
	result := FMapOption(opt, f)
	return InjectOption(result)
}

// ─── Practical: What You Can Do Without HKT ───────────────────────────────────
//
// In practice, Go programmers write per-type combinators.
// The iterator pattern (channels, slices) is idiomatic.

type Iter[T any] func(yield func(T) bool)

func IterFromSlice[T any](s []T) Iter[T] {
	return func(yield func(T) bool) {
		for _, v := range s {
			if !yield(v) {
				return
			}
		}
	}
}

func IterMap[A, B any](iter Iter[A], f func(A) B) Iter[B] {
	return func(yield func(B) bool) {
		iter(func(a A) bool {
			return yield(f(a))
		})
	}
}

func IterFilter[A any](iter Iter[A], pred func(A) bool) Iter[A] {
	return func(yield func(A) bool) {
		iter(func(a A) bool {
			if pred(a) {
				return yield(a)
			}
			return true
		})
	}
}

func IterCollect[T any](iter Iter[T]) []T {
	var result []T
	iter(func(v T) bool {
		result = append(result, v)
		return true
	})
	return result
}

func main() {
	// Option mapping
	opt := Some(21)
	doubled := FMapOption(opt, func(x int) int { return x * 2 })
	fmt.Printf("FMapOption(Some(21), *2) = %d, isSome=%v\n", doubled.value, doubled.isSome)

	// Via Apply (defunctionalization)
	applied := InjectOption(Some(10))
	result := FmapViaApply(applied, func(x int) string { return fmt.Sprintf("value=%d", x) })
	projected := ProjectOption(result)
	fmt.Printf("Via Apply: %s\n", projected.value)

	// Iterator functor pipeline
	nums := IterFromSlice([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	pipeline := IterMap(
		IterFilter(nums, func(x int) bool { return x%2 == 0 }),
		func(x int) string { return fmt.Sprintf("even:%d", x) },
	)
	fmt.Println("Even numbers:", IterCollect(pipeline))
}
```

### Go-specific considerations

Go made a deliberate decision not to include HKT. The Go FAQ and design documents state that HKT adds significant type system complexity for benefits that Go's core use cases (systems programming, microservices, tooling) do not often need. The consequence:

- Every container type (`Option`, `Result`, `Future`) requires its own `Map`, `FlatMap`, `Filter` implementations
- No single `Functor` interface can be written
- The `iter` package (1.23) provides a range-based iterator protocol that is functorial but not expressed as a Functor typeclass
- Libraries that want FP combinators must use code generation or per-type implementations

This is a documented limitation of Go generics, not a bug. The design prioritizes readability and compile speed over abstraction depth.

## Implementation: Rust

```rust
// Rust GATs let us express HKT in trait definitions.
// This is the closest Rust gets to Haskell's Functor/Monad typeclasses.

use std::fmt::Debug;

// ─── Functor via GATs ─────────────────────────────────────────────────────────

trait Functor {
    type Inner;
    // GAT: Mapped<B> is the "same container" but holding B instead of Inner
    type Mapped<B>;

    fn fmap<B, F: FnOnce(Self::Inner) -> B>(self, f: F) -> Self::Mapped<B>;
}

// Implement for Option — the most natural functor
impl<A> Functor for Option<A> {
    type Inner = A;
    type Mapped<B> = Option<B>;

    fn fmap<B, F: FnOnce(A) -> B>(self, f: F) -> Option<B> {
        self.map(f)
    }
}

// Implement for Vec
impl<A> Functor for Vec<A> {
    type Inner = A;
    type Mapped<B> = Vec<B>;

    fn fmap<B, F: FnMut(A) -> B>(self, mut f: F) -> Vec<B> {
        self.into_iter().map(|x| f(x)).collect()
    }
}

// Note: FnOnce vs FnMut — Vec::fmap needs FnMut because it calls f multiple times.
// This is a limitation of the trait as written; production implementations
// would use a more refined bound.

// ─── Monad via GATs ───────────────────────────────────────────────────────────

trait Monad: Functor {
    fn pure(value: Self::Inner) -> Self;

    // bind/and_then/flat_map: the key monadic operation
    fn bind<B, F>(self, f: F) -> Self::Mapped<B>
    where
        F: FnOnce(Self::Inner) -> Self::Mapped<B>,
        Self::Mapped<B>: Monad<Inner = B>;
}

impl<A> Monad for Option<A> {
    fn pure(value: A) -> Option<A> { Some(value) }

    fn bind<B, F>(self, f: F) -> Option<B>
    where
        F: FnOnce(A) -> Option<B>,
        Option<B>: Monad<Inner = B>,
    {
        self.and_then(f)
    }
}

// ─── Higher-Kinded Functions via GATs ─────────────────────────────────────────
// With GATs, we can write functions that are generic over the Functor:

fn fmap_twice<F: Functor<Inner = i32>>(
    container: F,
    f: impl FnOnce(i32) -> i32 + Clone,
) -> F::Mapped<i32>
where
    F::Mapped<i32>: Functor<Inner = i32, Mapped<i32> = F::Mapped<i32>>,
{
    container.fmap(f)
}

// ─── Type-Level Natural Numbers (Const Generics as Limited Dependent Types) ───
// Rust's const generics are not full HKT, but they give us type constructors
// that carry value-level information.

// A vector of known compile-time length: a type constructor of kind * × ℕ → *
#[derive(Debug, Clone)]
struct TypedVec<T, const N: usize> {
    data: [T; N],
}

impl<T: Default + Copy + Debug, const N: usize> TypedVec<T, N> {
    fn new(data: [T; N]) -> Self {
        TypedVec { data }
    }

    // map: A × N → B × N — preserves the length N at type level
    fn map<U: Default + Copy, F: Fn(T) -> U>(&self, f: F) -> TypedVec<U, N> {
        let mut result = [U::default(); N];
        for (i, &v) in self.data.iter().enumerate() {
            result[i] = f(v);
        }
        TypedVec { data: result }
    }

    // zip: zips two same-length vectors — length preserved
    fn zip<U: Copy>(&self, other: &TypedVec<U, N>) -> TypedVec<(T, U), N>
    where
        (T, U): Default + Copy,
    {
        let mut result = [(T::default(), U::default()); N];
        for i in 0..N {
            result[i] = (self.data[i], other.data[i]);
        }
        TypedVec { data: result }
    }
}

// concat: concatenates two typed vectors — length is N+M at type level
fn concat<T: Default + Copy, const N: usize, const M: usize>(
    a: TypedVec<T, N>,
    b: TypedVec<T, M>,
) -> TypedVec<T, { N + M }> {
    let mut data = [T::default(); { N + M }];
    data[..N].copy_from_slice(&a.data);
    data[N..].copy_from_slice(&b.data);
    TypedVec { data }
}

// ─── Why serde Does Not Need HKT ─────────────────────────────────────────────
// serde works without HKT because it uses a visitor pattern combined with
// trait dispatch. The Deserializer trait carries the format information,
// and each data type implements Deserialize for itself.
// The key trick: Deserialize is not "deserialize from any container" —
// it is "deserialize from any format that implements Deserializer."
// This is horizontal composition (trait bounds), not vertical (HKT).

trait FakeDeserializer {
    fn deserialize_i32(&mut self) -> Result<i32, String>;
    fn deserialize_string(&mut self) -> Result<String, String>;
}

trait FakeDeserialize: Sized {
    fn deserialize<D: FakeDeserializer>(d: &mut D) -> Result<Self, String>;
}

impl FakeDeserialize for i32 {
    fn deserialize<D: FakeDeserializer>(d: &mut D) -> Result<i32, String> {
        d.deserialize_i32()
    }
}

struct MyStruct { id: i32, name: String }

impl FakeDeserialize for MyStruct {
    fn deserialize<D: FakeDeserializer>(d: &mut D) -> Result<Self, String> {
        let id   = d.deserialize_i32()?;
        let name = d.deserialize_string()?;
        Ok(MyStruct { id, name })
    }
}
// The derive macro generates exactly this code. No HKT needed.

// ─── Demo ─────────────────────────────────────────────────────────────────────

fn main() {
    // GAT-based Functor
    let opt: Option<i32> = Some(21);
    let doubled = opt.fmap(|x| x * 2);
    println!("fmap Option: {doubled:?}");

    let vec_: Vec<i32> = vec![1, 2, 3];
    let doubled_vec = vec_.fmap(|x| x * 2);
    println!("fmap Vec: {doubled_vec:?}");

    // Monad bind
    let result: Option<String> = Some(42)
        .bind(|x| if x > 0 { Some(x) } else { None })
        .bind(|x| Some(format!("positive: {x}")));
    println!("Monad bind: {result:?}");

    // Type-level length vectors
    let v1 = TypedVec::new([1i32, 2, 3]);
    let v2 = TypedVec::new([4i32, 5, 6]);
    let doubled = v1.map(|x| x * 2);
    println!("TypedVec map: {:?}", doubled.data);

    let zipped = v1.zip(&v2);
    println!("TypedVec zip: {:?}", zipped.data);

    let concatenated = concat(v1, v2);
    println!("TypedVec concat len={}: {:?}", concatenated.data.len(), concatenated.data);
    // Length 6 is known at compile time — TypedVec<i32, 6>
}
```

### Rust-specific considerations

- **GATs are stable since Rust 1.65** (November 2022): The Rust RFC 1598. They enable the `type Mapped<B>` pattern above but still have limitations — you cannot express full HKT constraints like `where Self::Mapped<B>: Functor<Inner = B>` in all cases without boxing or workarounds.
- **`typenum` crate**: Before const generics, type-level numbers were encoded as types: `typenum::U5` represents 5 at the type level. This allows `GenericArray<T, N>` where N is a type-level number. It is essentially dependent types by encoding.
- **Const generics limitations**: Const generics currently support integers, booleans, and chars. Operations on const generics (like `N + M`) require the `generic_const_exprs` feature (still unstable as of 2025). The concat example above uses it.
- **serde's design insight**: serde achieves its goal (serialize/deserialize any format) without HKT by using a *visitor* architecture. The `Serializer`/`Deserializer` traits are the abstractions over formats. The `Serialize`/`Deserialize` traits are the abstractions over types. The crossing of these two dimensions is handled by implementing both trait pairs, not by using HKT.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Native HKT | No — deliberate design decision | Partial via GATs (stable 1.65+) |
| Kind system | No formal kinds | Implicit (types, type constructors with GATs) |
| HKT simulation | Defunctionalization / witness types / Apply encoding | GATs in traits; `typenum` for type-level numbers |
| Const generics | No | Yes (stable: integers, bools, chars) |
| Type constructor abstraction | Not expressible in interfaces | Expressible via `type Mapped<B>` GAT |
| HKT in practice | Per-type combinators; no single Functor interface | GAT-based Functor/Monad traits usable, with limits |
| Type-level arithmetic | No | `generic_const_exprs` (unstable); `typenum` (stable) |

## Production Applications

- **`ndarray` (Rust)**: uses const generics for dimensionality: `ArrayBase<D>` where D is the dimension type. Adding or reducing dimensions is checked at compile time.
- **`nalgebra`**: A linear algebra library using type-level numbers (`typenum`) for matrix dimensions. `Matrix<f64, U3, U4>` is a 3×4 matrix; multiplying `Matrix<f64, U3, U4>` by `Matrix<f64, U4, U5>` gives `Matrix<f64, U3, U5>` — dimension checking at compile time.
- **Haskell's `Functor` typeclass**: The canonical HKT example. `fmap` is universally polymorphic over the functor. Every container in Haskell's ecosystem implements it, enabling code like `fmap (+1)` to work on lists, Maybe, Either, IO, etc.
- **Scala's `cats` library**: `cats.Functor`, `cats.Monad` are HKT typeclasses. Scala's type system has native HKT (`F[_]`). The design inspired Rust's GAT approach.
- **Go's `slices.Map`**: Does not exist in stdlib (as of 1.23) because without HKT there is no way to write a single `Map` that works for all slice types in a type-safe, composable way without generics + type constraints — but Go generics do make per-type slice maps idiomatic.

## Complexity Analysis

**Compilation time**: GATs in Rust add solver complexity. Each `where` clause with GATs requires the trait solver to enumerate implementations. Complex GAT constraints can slow compilation significantly. Profile with `cargo build --timings` when GAT-heavy code is slow.

**Cognitive overhead**: HKT is the highest abstraction level in this section. A junior developer reading `fn fmap_twice<F: Functor<Inner = i32>>` needs to understand kinds, GATs, and associated types simultaneously. Consider whether the abstraction is worth the maintenance cost for your team.

**HKT vs concrete types**: For most production Rust code, the concrete APIs (`Option::map`, `Result::map`, `Iterator::map`) are preferable to GAT-based abstractions. The GAT approach pays off when writing library code that is genuinely parametric over the container type — for example, a serialization framework.

## Common Pitfalls

1. **Trying to write Functor in Go and hitting the HKT wall**: You cannot. `interface { Map(func(A) B) SomeContainer[B] }` breaks because the return type `SomeContainer[B]` is not expressible as the same container as the receiver. Accept this and write per-type map functions.

2. **GAT lifetime constraints**: GAT implementations often need lifetime bounds: `type Mapped<'a, B>`. Forgetting to include lifetimes in the GAT produces confusing "associated type lifetime parameters" errors. Always check whether your GAT needs lifetime parameters.

3. **Mistaking const generics for full dependent types**: `TypedVec<T, N>` where N is a `const usize` is not a dependent type system. N must be known at compile time (it is a const expression). You cannot do `TypedVec<T, n>` where `n` is a runtime variable.

4. **`typenum` vs const generics**: `typenum` predates const generics and is more expressive (full type-level arithmetic), but it produces complex error messages and slow compilation for large N. Prefer const generics where available; fall back to `typenum` for cases that const generics cannot handle.

5. **Over-encoding with defunctionalization in Go**: The Apply/Brand pattern is intellectually interesting but produces code that is hard to read and provides no compiler guarantee that the projection is safe. In practice, per-type implementations with consistent naming (`FmapOption`, `FmapResult`) are preferable for Go.

## Exercises

**Exercise 1** (30 min): In Rust, implement `Functor` for `Result<A, E>` (mapping over Ok). Then write a function `apply_twice<F: Functor<Inner = i32, Mapped<i32> = F>>(container: F, f: impl Fn(i32) -> i32) -> F` that applies `f` twice. What GAT bounds do you need to ensure the output type matches the input type?

**Exercise 2** (2–4 h): Implement the `Traversable` typeclass in Rust using GATs:
```rust
trait Traversable: Functor {
    fn traverse<F: Functor, G: Fn(Self::Inner) -> F>(
        self, f: G
    ) -> F::Mapped<Self::Mapped<F::Inner>>;
}
```
Implement it for `Option` and `Vec`. The semantics: `traverse(opt, f)` where `f: A -> Result<B, E>` should return `Result<Option<B>, E>` — it "flips" the container and the effect. This is used in Rust for `collect::<Result<Vec<_>, _>>()`.

**Exercise 3** (4–8 h): Build a `Matrix<T, const ROWS: usize, const COLS: usize>` in Rust using const generics. Implement:
- `transpose() -> Matrix<T, COLS, ROWS>`
- `multiply<const K: usize>(other: Matrix<T, COLS, K>) -> Matrix<T, ROWS, K>` (matrix multiplication with compile-time dimension checking)
- `map<U, F: Fn(T) -> U>(f: F) -> Matrix<U, ROWS, COLS>`

Verify that multiplying incompatible dimensions produces a compile error.

**Exercise 4** (8–15 h): Implement a type-safe heterogeneous list (HList) in Rust:
```rust
struct HNil;
struct HCons<H, T>(H, T);
```
Implement `Functor`-like `map` that applies a type-class-constrained function to each element. Use GATs to express the output type. Add `length()` as a const generic. This is the type-level foundation of `serde`'s struct visitor and `frunk`'s HList.

## Further Reading

### Foundational Papers
- Yallop, J., & White, L. (2014). "Lightweight higher-kinded polymorphism." *FLOPS 2014*. — The defunctionalization technique for simulating HKT in languages without it (OCaml). Directly applicable to Go.
- Chakravarty, M. et al. (2005). "Associated type synonyms." *ICFP 2005*. — The Haskell paper that introduced associated types, predecessor to GATs.
- Rust RFC 1598: "Generic associated types." — The specification for GATs in Rust. Contains the motivation and design rationale.

### Books
- *Types and Programming Languages* (Pierce) — Chapter 29 covers type operators and kinding: the formal foundation for HKT.
- Milewski, B. *Category Theory for Programmers* — Chapter 15 covers the Yoneda lemma, which explains why defunctionalization works.

### Production Code to Read
- `nalgebra` crate — `src/base/matrix.rs`: const-generic matrix type. Shows the production use of type-level dimension checking.
- `functor_derive` crate (Rust) — A derive macro for `Functor` using GATs. Shows the generated code pattern.
- `frunk` crate (Rust) — `src/hlist.rs`: the HList implementation. Study how `fmap` is implemented on a heterogeneous list.

### Talks
- "GATs in Rust: A New Level of Type-Level Programming" (Jack Huey, RustConf 2022) — the GAT RFC author explains the design and current limitations.
- "Higher-Kinded Types: The Functionality Functor" (Rúnar Bjarnason, Scala Days 2015) — why HKT matters for functional library design.
