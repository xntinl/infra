# 6. Const Generics and Compile-Time Computation

**Difficulty**: Insane

## The Challenge

Push the Rust compiler into doing as much work as possible before your program ever
runs. Build a linear algebra library where matrix dimensions are checked at compile
time, a compile-time regular expression validator, and a const-evaluated data
structure — all using const generics and `const fn`.

Const generics (stabilized in Rust 1.51 for primitive types) let types be
parameterized by values, not just other types. `const fn` lets functions execute at
compile time. Together, they replace entire categories of runtime checks with
compile-time guarantees and eliminate entire categories of runtime overhead.

But the current implementation has sharp edges: `generic_const_exprs` is still
unstable, `const fn` cannot do everything a regular function can, and the compiler's
const evaluator has limits. You will learn exactly where these boundaries are by
hitting them.

## Acceptance Criteria

### Part 1: Const Generic Matrix Library
- [ ] Define `Matrix<T, const ROWS: usize, const COLS: usize>` backed by
      `[[T; COLS]; ROWS]`
- [ ] Implement `Add` for matrices with matching dimensions — mismatched dimensions
      are a compile error
- [ ] Implement `Mul` for `Matrix<T, M, N> * Matrix<T, N, P>` producing
      `Matrix<T, M, P>` — the inner dimension `N` must match at compile time
- [ ] Implement `transpose()` returning `Matrix<T, COLS, ROWS>`
- [ ] Implement `identity()` returning `Matrix<T, N, N>` — only available for square
      matrices
- [ ] Demonstrate: `Matrix::<f64, 2, 3> * Matrix::<f64, 3, 4>` compiles;
      `Matrix::<f64, 2, 3> * Matrix::<f64, 4, 5>` does not
- [ ] Demonstrate: the compiler error for dimension mismatch is readable and
      points to the multiplication site

### Part 2: Compile-Time Assertions and Validation
- [ ] Write a `const fn` that validates a byte string is valid UTF-8 at compile time
- [ ] Use `const { assert!(...) }` blocks (Rust 1.79+) to enforce invariants that
      trigger compile errors, not panics
- [ ] Build a `NonEmptyArray<T, const N: usize>` type where `N = 0` is a compile error
      via `const { assert!(N > 0) }` in the constructor
- [ ] Build a const-evaluated lookup table: a `const fn` that computes a CRC32 table
      at compile time, stored in a `static` — verify it matches a runtime-computed
      version
- [ ] Demonstrate the `const_eval_limit` by writing a `const fn` that exceeds it,
      then show the attribute to increase it

### Part 3: Const Fn Data Structures
- [ ] Implement a `const fn`-constructible fixed-capacity stack:
      `ConstStack<T, const CAP: usize>` with `push`, `pop`, `peek`, `len` — all
      `const fn`
- [ ] The stack must work in both const and runtime contexts
- [ ] Demonstrate: `const STACK: ConstStack<i32, 10> = ConstStack::new().push(1).push(2).push(3);`
      compiles and `STACK.peek()` returns `3`
- [ ] Implement a `const fn` sorting algorithm (e.g., insertion sort) that sorts an
      array at compile time
- [ ] Demonstrate: `const SORTED: [i32; 5] = const_sort([5, 3, 1, 4, 2]);` produces
      `[1, 2, 3, 4, 5]` with zero runtime cost

### Part 4: Const Generics in Trait Implementations
- [ ] Implement `From<[T; N]>` for your `ConstStack<T, N>` using const generics
- [ ] Implement a trait `Flatten` that converts `Matrix<T, 1, N>` to `[T; N]` and
      `Matrix<T, N, 1>` to `[T; N]` — both row and column vectors
- [ ] Implement `Display` for `Matrix` with const-generic dimensions
- [ ] Explore the `generic_const_exprs` nightly feature: implement matrix
      concatenation where `Matrix<T, M, N>.hstack(Matrix<T, M, P>)` returns
      `Matrix<T, M, {N + P}>` — this requires nightly

### Part 5: Const Generics vs Typenum
- [ ] Reimplement your matrix multiplication using `typenum` instead of const generics
- [ ] Compare: compile-time error messages, compilation speed (measure with
      `cargo build --timings`), ergonomics of the API
- [ ] Document the current limitations of const generics that make `typenum` still
      necessary in some cases (as of Rust 1.82+)

## Starting Points

- **nalgebra** (`dimforge/nalgebra`): Study `src/base/dimension.rs` and
  `src/base/matrix.rs`. nalgebra uses a hybrid approach with both const generics and
  its own `Const<N>` wrapper. Understand why — the `generic_const_exprs` feature
  being unstable forces this compromise.
- **Rust Reference: Const Generics**: The `const_generics` section of the Rust
  Reference documents what types are allowed as const parameters and the current
  restrictions on const expressions in type positions.
- **generic_const_exprs RFC** (rust-lang/rfcs#2000): This RFC proposes allowing
  arbitrary const expressions in generic positions (e.g., `Array<{N + 1}>`). Read
  the motivation and current blockers — this is the missing piece that would make
  const generics fully replace typenum.
- **const_eval docs**: Study `compiler/rustc_const_eval/src/` in the rust-lang/rust
  repository to understand the Miri-based const evaluator and its limitations.
- **The `const-sha1` crate**: A SHA-1 implementation that runs entirely at compile
  time. Study `src/lib.rs` for techniques on writing complex `const fn` code within
  the current limitations.

## Hints

1. The key limitation of stable const generics: you cannot use const generic
   parameters in const expressions in type position. `fn foo<const N: usize>() ->
   [u8; N + 1]` does not compile on stable. You can work around this by taking two
   const parameters: `fn foo<const N: usize, const N_PLUS_1: usize>()` and asserting
   `N + 1 == N_PLUS_1` at compile time. Ugly, but it works on stable.

2. `const fn` limitations (as of Rust 1.82): no trait method calls (only inherent
   methods), no `for` loops over iterators (but `while` and `loop` work), no
   `dyn Trait`, limited `match` on non-primitive types. You will write a lot of
   `while` loops with index variables instead of `.iter().map()` chains. Accept this.

3. For the matrix library, you do not need `generic_const_exprs` for the basic
   operations. `Add` works because both matrices share the same `ROWS` and `COLS`.
   `Mul` works because the inner dimension `N` is a shared const parameter. The
   compiler unifies the const parameters during type checking. Concatenation is where
   you hit the wall.

4. `const { assert!(N > 0) }` is a `const` block (stabilized in Rust 1.79). It
   evaluates at compile time even in a non-const context. This is different from
   `assert!` in a `const fn`, which only evaluates at compile time when called from
   a const context. The `const {}` block is unconditionally evaluated at compile time.

5. For the const-evaluated CRC32 table: the key technique is a `const fn` that takes
   no parameters and returns `[u32; 256]`, using a `while` loop with a mutable
   index. Assign the result to a `static TABLE: [u32; 256] = compute_table();` and
   the compiler evaluates `compute_table()` at compile time, embedding the 1KB table
   directly in the binary.

## Going Further

- Implement a compile-time parser: a `const fn` that parses a format string
  (like `"{} + {} = {}"`) and returns a type-level representation of the expected
  arguments. This is approximately what `std::fmt` does with the `format_args!`
  macro.
- Build a fixed-point arithmetic library `FixedPoint<const INTEGER_BITS: u32, const
  FRACTIONAL_BITS: u32>` where all operations preserve correct bit widths at compile
  time. Useful in embedded/DSP contexts.
- Implement a const-generic ring buffer `RingBuffer<T, const N: usize>` where `N`
  must be a power of 2 (enforced at compile time) so that modular arithmetic can
  use bitwise AND instead of modulo.
- Explore `adt_const_params` (nightly): this allows types like `&'static str` and
  custom structs as const generic parameters. Build a type-safe SQL query builder
  where table and column names are const generic string parameters.
- Write a const-evaluable brainfuck interpreter that runs a brainfuck program
  entirely at compile time and embeds the output in the binary.

## Resources

- [Rust Reference: Const Generics](https://doc.rust-lang.org/reference/items/generics.html#const-generics) —
  Official specification of const generics
- [RFC 2000: Const Generics](https://rust-lang.github.io/rfcs/2000-const-generics.html) —
  The original const generics RFC
- [generic_const_exprs tracking issue](https://github.com/rust-lang/rust/issues/76560) —
  The unstable feature for const expressions in type positions
- [nalgebra source](https://github.com/dimforge/nalgebra) — Production linear algebra
  library navigating const generic limitations
- [typenum source](https://github.com/paholg/typenum) — The pre-const-generics approach
  to type-level integers
- [const-sha1 source](https://github.com/rylev/const-sha1) — Complex compile-time
  computation in a const fn
- [Niko Matsakis: "Const Generics MVP"](https://blog.rust-lang.org/2021/02/26/const-generics-mvp-beta.html) —
  The stabilization announcement with a clear explanation of what was and was not included
- [Jack Wrenn: "Generalizing over Generics in Rust"](https://jackh726.github.io/rust/2022/05/04/a-shiny-future-with-const-generics.html) —
  On the future of const generics and their interaction with the trait system
- [Rust Compiler Const Eval](https://rustc-dev-guide.rust-lang.org/const-eval.html) —
  The rustc dev guide section on how const evaluation works internally
- [Rust 1.79 Release Notes](https://blog.rust-lang.org/2024/06/13/Rust-1.79.0.html) —
  `const {}` blocks stabilization
