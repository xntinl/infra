# 4. Type-Level Programming

**Difficulty**: Insane

## The Challenge

Encode computations in Rust's type system so that invalid states are rejected at
compile time, not at runtime. Build a compile-time dimensional analysis library
where the compiler itself prevents you from adding meters to seconds.

Rust's type system is powerful enough to perform arithmetic, enforce state machine
transitions, and build heterogeneous collections — all at zero runtime cost. The
techniques here sit at the boundary between "clever library design" and "abuse of
the type checker," and understanding where that line is drawn will make you a
significantly better API designer.

You will build four things, each layering on the previous:

1. **Type-level natural numbers** using Peano encoding
2. **A heterogeneous list** (HList) with type-safe indexing
3. **A type-state builder** that enforces required fields at compile time
4. **A dimensional analysis library** where units are tracked in the type system

## Acceptance Criteria

### Type-Level Naturals (Peano)
- [ ] Define `Zero` and `Succ<N>` types representing natural numbers
- [ ] Implement type-level addition: `Add<A, B>` where the result is computed by
      the type checker (e.g., `<Succ<Succ<Zero>> as Add<Succ<Zero>>>::Result`
      resolves to `Succ<Succ<Succ<Zero>>>`)
- [ ] Implement type-level comparison: `IsLess<A, B>` that resolves to `True` or
      `False` at the type level
- [ ] Demonstrate: a function that accepts a type-level natural `N` and returns an
      array of exactly `N` elements (using const generics to bridge the gap)

### Heterogeneous List (HList)
- [ ] Define `HNil` and `HCons<Head, Tail>` types
- [ ] Implement `hlist![1, "hello", 3.14]` macro that constructs an
      `HCons<i32, HCons<&str, HCons<f64, HNil>>>`
- [ ] Implement type-safe indexing: `hlist.get::<Here>()` returns the head,
      `hlist.get::<There<Here>>()` returns the second element, with correct types
- [ ] Implement `Map` that applies a polymorphic function to each element
- [ ] The HList must be fully heterogeneous — different types at each position

### Type-State Builder
- [ ] Design a builder for a `Connection` type with required fields `host`, `port`,
      and optional field `timeout`
- [ ] The builder tracks which fields have been set in the type system using
      `PhantomData` markers
- [ ] Calling `.build()` is only possible when all required fields are set — the
      method literally does not exist on the type until the precondition is met
- [ ] Setting a field twice is a compile error
- [ ] Demonstrate: show the exact compiler error when trying to build without a
      required field

### Dimensional Analysis
- [ ] Represent SI dimensions (length, time, mass) as type-level integers
      (e.g., `Quantity<Length = P1, Time = N2, Mass = Zero>` for acceleration m/s^2)
- [ ] Addition and subtraction require identical dimensions — enforced at compile time
- [ ] Multiplication and division produce correct result dimensions automatically
      (meters * meters = meters^2)
- [ ] Provide `Meters`, `Seconds`, `Kilograms` type aliases
- [ ] Demonstrate: `let v = distance / time;` produces a velocity type, and
      `let nonsense = distance + time;` is a compile error with a readable message
- [ ] Support at least two unit systems and conversion between them (e.g., meters
      to feet with a const conversion factor)

## Starting Points

- **typenum** (`paholg/typenum`): Study `src/int.rs` and `src/uint.rs`. This crate
  encodes integers in the type system using a binary representation (not Peano).
  Understand the `op!` macro and how `Add` is implemented for type-level integers.
  Your Peano encoding is simpler but slower to compile — typenum exists because
  Peano does not scale.
- **frunk** (`lloydmeta/frunk`): Study `core/src/hlist.rs` for HList and
  `core/src/coproduct.rs` for type-level sum types. The `Sculptor` trait that
  allows reordering HList fields is particularly elegant — study how it uses
  recursive trait resolution.
- **dimensioned** (`paholg/dimensioned`): A dimensional analysis library built on
  typenum. Study how dimensions are represented as type-level integer tuples.
- **uom** (`iliekturtles/uom`): "Units of Measurement" — a more complete
  dimensional analysis library. Study `src/si/` for how SI units are defined and
  how the macro system generates unit types.
- **Rust Reference: PhantomData** — Read the variance table in the `PhantomData`
  docs. `PhantomData<T>` is covariant in `T`, `PhantomData<fn(T)>` is contravariant,
  `PhantomData<fn(T) -> T>` is invariant. Choose wrong and your type-state API will
  have unsoundness holes.

## Hints

1. Peano addition is defined recursively on the type level:
   `Add<Zero, B> = B` and `Add<Succ<A>, B> = Succ<Add<A, B>>`. This maps directly
   to trait impls with associated types. The trait solver does the "computation"
   at compile time by resolving the associated type chain.

2. For HList indexing, define a trait `Pluck<Index>` with an associated type `Output`.
   The index is itself a type: `Here` (base case) and `There<I>` (recursive case).
   `HCons<H, T>` implements `Pluck<Here>` returning `H`, and `Pluck<There<I>>`
   delegating to `T::Pluck<I>`. This compile-time recursion is how frunk's
   `Selector` trait works.

3. The type-state builder uses phantom type parameters to track state:
   `Builder<HostSet, PortSet, TimeoutSet>` where each parameter is either `Set` or
   `Unset`. The `build()` method is only implemented for
   `Builder<Set, Set, TimeoutSet>` (any timeout state). Use `impl` blocks with
   concrete type parameters to control method availability.

4. For dimensional analysis, represent each quantity as `Quantity<L, T, M, f64>`
   where `L`, `T`, `M` are type-level integers. Implement `Add` only when
   `L1 = L2, T1 = T2, M1 = M2`. Implement `Mul` to produce
   `Quantity<L1 + L2, T1 + T2, M1 + M2>`. The type-level integer addition you
   already built feeds directly into this.

5. Compile-time error messages in type-level programming are notoriously bad. Use
   trait bounds with descriptive names: `where Lhs: SameDimensionAs<Rhs>` produces
   a better error than `where Lhs::Length: IsEqual<Rhs::Length>`. You can also use
   `compile_error!` in strategic positions.

## Going Further

- Implement GAT-based higher-kinded type simulation: define a `Functor` trait using
  GATs (`type Output<B>`) and implement it for `Option`, `Vec`, and `Result<_, E>`.
  This is the closest Rust gets to Haskell's `Functor` typeclass.
- Build a type-level state machine for a network protocol (TCP handshake: `Closed ->
  SynSent -> Established -> FinWait -> Closed`) where invalid transitions are
  compile errors.
- Extend your dimensional analysis to support fractional exponents (e.g., square
  root of area gives length) using type-level rationals.
- Implement type-level sorting on HLists — given `hlist![3, 1, 2]` as type-level
  naturals, produce a sorted HList at compile time.
- Study how the `diesel` ORM uses type-level programming to make invalid SQL queries
  unrepresentable. Look at `diesel/src/query_builder/` and the `Queryable` derive macro.

## Resources

- [typenum source](https://github.com/paholg/typenum) — Type-level numbers in
  binary encoding
- [frunk source](https://github.com/lloydmeta/frunk) — HList, Coproduct, Generic
  derivation
- [uom source](https://github.com/iliekturtles/uom) — Units of measurement via
  type-level programming
- [Rust RFC 1598: GATs](https://rust-lang.github.io/rfcs/1598-generic_associated_types.html) —
  The feature that enables HKT simulation
- [Rust Reference: Subtyping and Variance](https://doc.rust-lang.org/reference/subtyping.html) —
  Essential for understanding PhantomData's role
- [Alexis King: "Parse, Don't Validate"](https://lexi-lambda.github.io/blog/2019/11/05/parse-don-t-validate/) —
  The philosophy behind type-state programming
- [Will Crichton: "Type-Level Programming in Rust"](https://willcrichton.net/notes/type-level-programming/) —
  Excellent overview of the techniques
- [Rustconf 2018: "Closing Keynote" by Catherine West](https://www.youtube.com/watch?v=aKLntZcp27M) —
  On pushing type systems to enforce invariants in game engines
- [Yaron Minsky: "Make Illegal States Unrepresentable"](https://blog.janestreet.com/effective-ml-revisited/) —
  The OCaml talk that inspired the type-state movement
