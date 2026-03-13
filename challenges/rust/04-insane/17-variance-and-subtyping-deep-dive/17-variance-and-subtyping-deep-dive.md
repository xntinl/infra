# 17. Variance and Subtyping Deep Dive

**Difficulty**: Insane

## The Challenge

Variance is one of the most subtle and misunderstood aspects of Rust's type system. While most Rust programmers never think about it directly, variance governs how lifetimes interact with generic types, determines when you can pass a `&'long T` where a `&'short T` is expected, and is the reason certain seemingly-reasonable patterns fail to compile. The rules are enforced silently by the compiler, and when they bite you, the error messages rarely say "this is a variance problem."

Your task is to build a comprehensive demonstration library called `variance_lab` that exercises every form of variance in Rust --- covariance, contravariance, and invariance --- through carefully constructed types, traits, and functions. You will create types that are covariant, contravariant, and invariant in their lifetime and type parameters, prove each classification through compile-pass and compile-fail tests, and build a test harness that documents exactly why each case behaves the way it does. This is not a toy exercise: you must demonstrate that you understand how the compiler computes variance, how `PhantomData` influences it, and how variance interacts with trait objects, higher-ranked trait bounds (HRTBs), and unsafe code.

Beyond the demonstration types, you will implement a practical component: a typed heterogeneous container (like a type map) where variance correctness is essential to soundness. You must prove that your container cannot be used to extend lifetimes, transmute types, or otherwise violate safety --- and you must write tests that would catch such violations if your variance annotations were wrong. The goal is not just to understand variance intellectually but to build something where getting it wrong leads to unsoundness, and getting it right is the only path to a compiling, safe library.

## Acceptance Criteria

- [ ] Create a library crate `variance_lab` with clearly separated modules: `covariant`, `contravariant`, `invariant`, `phantom`, `practical`, and `proofs`
- [ ] In the `covariant` module, define at least three types that are covariant in a lifetime parameter, demonstrating that `T<'long>` can be used where `T<'short>` is expected
- [ ] In the `covariant` module, define at least two types that are covariant in a type parameter, demonstrating that `Container<&'long str>` can be used where `Container<&'short str>` is expected
- [ ] In the `contravariant` module, define at least two types that are contravariant in a lifetime parameter, using function pointers (`fn(&'a T)`) or equivalent constructions
- [ ] Demonstrate that for a contravariant type `C<'a>`, you can use `C<'short>` where `C<'long>` is expected (the opposite of covariance)
- [ ] In the `invariant` module, define at least three types that are invariant in a lifetime parameter, using `Cell<&'a T>`, `&'a mut T`, or equivalent constructions
- [ ] Prove invariance by showing that neither `I<'long> -> I<'short>` nor `I<'short> -> I<'long>` compiles
- [ ] In the `phantom` module, demonstrate how `PhantomData<T>`, `PhantomData<fn(T)>`, `PhantomData<fn() -> T>`, `PhantomData<fn(T) -> T>`, `PhantomData<*mut T>`, and `PhantomData<Cell<T>>` each affect variance
- [ ] For every `PhantomData` variant, include a compile-pass test confirming the expected subtyping direction works, and a compile-fail test confirming the forbidden direction does not
- [ ] Implement compile-fail tests using `trybuild` (the `trybuild` crate) with `.rs` files in a `tests/ui/` directory and corresponding `.stderr` files for expected error output
- [ ] Include at least 15 compile-fail test cases covering: invariance blocking lifetime shortening, invariance blocking lifetime lengthening, contravariance blocking the covariant direction, mismatched variance in struct fields, and unsoundness attempts on the practical container
- [ ] Include at least 10 compile-pass test cases proving that the correct subtyping directions work for each variance category
- [ ] In the `proofs` module, implement a `Variance` trait with associated type machinery that lets you query the variance of a type parameter at the type level (this is an approximation --- use marker types `Covariant`, `Contravariant`, `Invariant` and implement the trait for your demonstration types)
- [ ] Demonstrate how `&'a T` is covariant in both `'a` and `T`, but `&'a mut T` is covariant in `'a` and invariant in `T`
- [ ] Build a type that is covariant in one parameter and invariant in another, and prove both properties with tests
- [ ] Build a type that is covariant in one parameter and contravariant in another, and prove both properties with tests
- [ ] Show how trait objects (`dyn Trait + 'a`) interact with variance: demonstrate that `Box<dyn Trait + 'long>` can be used where `Box<dyn Trait + 'short>` is expected
- [ ] Demonstrate variance behavior with higher-ranked trait bounds: show how `for<'a> fn(&'a T)` differs from `fn(&'specific T)` in terms of subtyping
- [ ] In the `practical` module, implement a `TypeMap` that maps `TypeId` to values with lifetime tracking, where:
  - Inserting a value with lifetime `'a` returns a handle bound to `'a`
  - Getting a value returns a reference bound to the correct lifetime
  - The container is invariant in the lifetime of its contents (to prevent unsoundness)
  - Interior mutability (`RefCell` or similar) is used, requiring careful variance reasoning
- [ ] Write at least 5 tests for `TypeMap` proving it cannot be used to:
  - Extend a lifetime beyond its original scope
  - Retrieve a value as a different type
  - Hold a dangling reference after the source is dropped
  - Create aliased mutable references
  - Transmute between types via lifetime manipulation
- [ ] Write a module-level documentation comment for every module explaining the variance rules it demonstrates, using ASCII diagrams to show subtyping relationships
- [ ] Include a top-level `lib.rs` doc comment that serves as a comprehensive tutorial on variance in Rust (at least 80 lines of documentation)
- [ ] Every type must have a doc comment explaining its variance in each parameter and why
- [ ] No `unsafe` code except where specifically demonstrating how wrong variance leads to unsoundness (and those uses must be clearly marked and behind `#[cfg(test)]`)
- [ ] All safe code compiles on stable Rust (nightly allowed only for compile-fail test tooling if needed)
- [ ] `cargo test` passes with all compile-pass tests succeeding and all compile-fail tests producing the expected errors
- [ ] Include a `README.md` in the crate root summarizing what each module covers and providing a reading order

### Advanced Variance Scenarios

- [ ] Build a `CallbackStore<'a, T>` type that stores `Vec<Box<dyn Fn(&'a T)>>` and demonstrate its contravariance in `'a` (you can store a callback that accepts `&'short T` where a callback accepting `&'long T` is expected, because the callback with the shorter requirement is more general)
- [ ] Build a `BidirectionalChannel<'a, T>` type that both sends and receives `&'a T`, prove it is invariant in `'a`, and explain why in documentation
- [ ] Demonstrate variance through three levels of nesting: `Outer<Middle<Inner<'a>>>` where each layer contributes differently to the final variance, and write a test proving the computed variance is correct
- [ ] Create a `LifetimePhantom<'a>` type using only `PhantomData` (no real data) that is contravariant in `'a`, and use it as a bound witness in a function signature to restrict callers
- [ ] Build a `ReadOnly<'a, T>` vs `ReadWrite<'a, T>` pair where `ReadOnly` is covariant in `T` and `ReadWrite` is invariant in `T`, mirroring the `&T` vs `&mut T` distinction but with your own custom types
- [ ] Demonstrate that `fn(T) -> T` is invariant in `T` (not bivariant), by showing that neither `fn(&'long str) -> &'long str` nor `fn(&'short str) -> &'short str` can substitute for the other
- [ ] Write a test that exercises variance through `Option<T>`, `Result<T, E>`, `Vec<T>`, `HashMap<K, V>`, `Rc<T>`, and `Arc<T>`, proving that each standard library container preserves the variance of its parameters
- [ ] Create a pair of types `Producer<'a>` (covariant) and `Consumer<'a>` (contravariant) and show that combining them in a struct yields invariance in `'a`
- [ ] Demonstrate the interaction between variance and closures: show that `impl Fn(&'a str) -> &'a str` has different variance behavior than `fn(&'a str) -> &'a str` due to closure capture semantics
- [ ] Build a compile-fail test showing that a hypothetical "covariant mutable reference" wrapper (using unsafe + wrong PhantomData) would allow creating dangling references, proving that invariance of `&mut T` in `T` is necessary for soundness
- [ ] Implement a `Branded<'brand, T>` type (inspired by the GhostCell/branded indices pattern) where `'brand` is invariant and used to tie values to a unique scope, preventing cross-scope confusion
- [ ] Write at least 3 tests demonstrating how variance interacts with `where` clauses and trait bounds: show cases where adding a bound changes whether a subtyping relationship holds

## Starting Points

- Study the [Rustonomicon chapter on subtyping and variance](https://doc.rust-lang.org/nomicon/subtyping.html) --- this is the canonical reference for how Rust computes variance and is the foundation for everything in this exercise
- Read the [rustc source for variance computation](https://github.com/rust-lang/rust/tree/master/compiler/rustc_hir_analysis/src/variance) --- specifically `constraints.rs` which builds the variance constraints and `solve.rs` which solves them; understanding the algorithm will clarify edge cases
- Study the [`PhantomData` documentation](https://doc.rust-lang.org/std/marker/struct.PhantomData.html) and the [nomicon chapter on `PhantomData`](https://doc.rust-lang.org/nomicon/phantom-data.html) for the variance implications of each `PhantomData` form
- Read [Niko Matsakis's blog post on variance](https://smallcultfollowing.com/babysteps/blog/2014/05/13/focusing-on-ownership/) for historical context on how Rust's variance rules were designed
- Study the [`trybuild` crate documentation](https://docs.rs/trybuild/latest/trybuild/) for how to write compile-fail tests --- look at how `serde_derive` and `thiserror` use `trybuild` for their test suites
- Examine the [Stacked Borrows paper by Ralf Jung](https://plv.mpi-sws.org/rustbelt/stacked-borrows/) for understanding how mutable reference invariance connects to Rust's aliasing model
- Read the [variance section of the Rust Reference](https://doc.rust-lang.org/reference/subtyping.html) for the formal rules
- Study [dtolnay/semver-trick](https://github.com/dtolnay/semver-trick) for creative uses of variance in real crate design
- Look at how the [`typed-arena` crate](https://docs.rs/typed-arena/latest/typed_arena/) handles lifetime invariance to prevent dangling references
- Examine [typemap crates](https://docs.rs/typemap-ors/latest/typemap_ors/) for the practical `TypeMap` component
- Study the [GhostCell paper](https://plv.mpi-sws.org/rustbelt/ghostcell/) by Joshua Yanovski et al. for how branded lifetimes (invariant lifetime parameters) can enforce safety properties at zero runtime cost
- Read [Ralf Jung's blog post on variance](https://www.ralfj.de/blog/2019/04/10/variance.html) for a rigorous treatment connecting variance to the semantic model of Rust types
- Study the [variance tests in the rustc test suite](https://github.com/rust-lang/rust/tree/master/tests/ui/variance) for examples of how the compiler team tests variance behavior --- these can serve as inspiration for your own test cases
- Examine how the [`ghost-cell` crate](https://docs.rs/ghost-cell/latest/ghost_cell/) uses branded invariant lifetimes in practice

## Hints

1. Start by building a mental model of Rust's subtyping lattice: `'static` is the "longest" lifetime and is a subtype of every other lifetime. If `'long: 'short`, then `'long` is a subtype of `'short`. Covariance preserves this ordering, contravariance reverses it, invariance ignores it entirely.

2. The simplest covariant type is `&'a T` --- it is covariant in both `'a` and `T`. The simplest invariant type is `&'a mut T` (invariant in `T`, covariant in `'a`). The simplest contravariant type is `fn(&'a T)` (contravariant in `'a`).

3. To prove covariance in tests, create a function that accepts `YourType<'short>` and pass it a `YourType<'long>`. If it compiles, the type is covariant (or bivariant). To prove it is not contravariant, show that passing `YourType<'short>` where `YourType<'long>` is expected fails to compile.

4. Variance is computed field-by-field by the compiler and then combined. If a struct has two fields, one covariant in `'a` and one invariant in `'a`, the struct is invariant in `'a` (invariance "wins"). The combination rules are: co + co = co, contra + contra = contra, co + contra = invariant, anything + invariant = invariant.

5. For `PhantomData` variance markers, remember: `PhantomData<T>` is covariant in `T`, `PhantomData<fn(T)>` is contravariant in `T`, `PhantomData<fn() -> T>` is covariant in `T`, `PhantomData<fn(T) -> T>` is invariant in `T`, and `PhantomData<*mut T>` is invariant in `T`. These are your tools for controlling variance in types that don't actually store `T`.

6. For compile-fail tests with `trybuild`, create a `tests/variance_ui.rs` file containing `#[test] fn ui() { let t = trybuild::TestCases::new(); t.compile_fail("tests/ui/*.rs"); }`. Each `.rs` file in `tests/ui/` should be a self-contained program that demonstrates a compile error. The `.stderr` file should match the compiler's error output.

7. When building the variance proof trait, you cannot actually inspect variance at the type level in Rust --- there is no built-in mechanism. Instead, use marker types and implement the trait manually for your types. The value is in forcing yourself to explicitly declare and verify the variance of each parameter.

8. For the `TypeMap`, the key insight is that you need the container to be invariant in the lifetime of references it stores. If it were covariant, a user could insert a `&'long str`, get back a `&'short str` (fine so far), but if the container is shared and uses interior mutability, covariance could allow unsound lifetime extension. Use `Cell` or `UnsafeCell` in the storage layer to force invariance.

9. To demonstrate HRTB interactions, contrast `fn foo(f: fn(&i32))` (which accepts `for<'a> fn(&'a i32)`) with `fn bar<'a>(f: fn(&'a i32))` (which accepts `fn(&'specific i32)`). The former is more general due to higher-ranked subtyping.

10. A common pitfall: `Vec<T>` is covariant in `T` because it only contains an owned `T` (through a pointer). But `Vec<&'a mut T>` is invariant in `T` and covariant in `'a` --- the `&mut` forces invariance on `T`. Make sure your tests cover nested variance through standard library types.

11. For the trait object variance tests, remember that `dyn Trait + 'a` is covariant in `'a`. This means `Box<dyn Display + 'static>` can be passed where `Box<dyn Display + 'a>` is expected for any `'a`. Test this explicitly.

12. Consider edge cases: what is the variance of `fn(fn(&'a T))`? It is covariant in `'a` because contravariance of contravariance is covariance. Build a test demonstrating this "double negation" property.

13. For your documentation, draw ASCII variance tables like:
    ```
    Type                    | 'a          | T
    ------------------------|-------------|------------
    &'a T                   | covariant   | covariant
    &'a mut T               | covariant   | invariant
    fn(&'a T)               | contra      | contra
    Cell<&'a T>             | invariant   | covariant
    fn(fn(&'a T))           | covariant   | covariant
    ```

14. When writing unsoundness demonstrations, show what would happen if variance were wrong. For example: "If `Cell<&'a T>` were covariant in `'a`, we could put a `&'long str` in a `Cell`, alias it, then replace the contents with `&'short str`, and read it through the alias as `&'long str` --- a dangling reference." Write this as a compile-fail test that proves the compiler prevents it.

15. The `TypeMap` should use `std::any::Any` for type erasure and `TypeId` for the key. The tricky part is tracking the lifetime: you cannot store `&'a T` as `Box<dyn Any>` because `Any: 'static`. Instead, consider using a custom trait `AnyRef<'a>` that erases the type but preserves the lifetime. This is where variance becomes practically critical.

16. Test that your `TypeMap` works correctly with types that have different variance properties: insert a `&str` (covariant in lifetime), a `&mut [u8]` (invariant in element type), and a `fn(&i32)` (contravariant). Verify that the container handles all of them without unsoundness.

17. For bonus rigor, use Miri (`cargo +nightly miri test`) to verify that your unsafe blocks (if any) do not cause undefined behavior. Miri can catch subtle aliasing violations that tests alone cannot.

18. Structure your compile-fail tests in a deliberate progression: start with simple lifetime subtyping failures, then variance-induced failures, then practical unsoundness attempts. Each test file should have a comment at the top explaining exactly what it tests and why it should fail.

19. Remember that bivariance (both covariant and contravariant, meaning the parameter is unused) exists in Rust but is rare. `PhantomData<fn(T) -> T>` is NOT bivariant --- it is invariant. Bivariance only occurs when a parameter truly does not appear in the type at all, which the compiler normally warns about.

20. The ultimate test of your understanding: can you construct a type that changes variance based on a const generic or type parameter? For example, `VarianceSwitch<T, const COVARIANT: bool>` that is covariant when `COVARIANT` is true and invariant otherwise. This is a stretch goal --- it may not be possible in current Rust, and explaining why is itself valuable.

21. For the `Branded<'brand, T>` type, the trick is to use an invariant lifetime parameter as a "brand" that ties a value to a specific scope. The classic construction uses a closure with a higher-ranked lifetime: `fn with_brand<R>(f: impl for<'brand> FnOnce(Token<'brand>) -> R) -> R`. Because `'brand` is universally quantified and invariant, the caller cannot smuggle branded values out of the closure.

22. When testing standard library containers, remember that `Rc<T>` and `Arc<T>` are covariant in `T` because they only provide shared (`&`) access. If they provided mutable access (like `Rc<RefCell<T>>`), the `RefCell` would make the combination invariant in `T`. This is a great test case for demonstrating how wrapping changes variance.

23. For the `CallbackStore` test, the key insight is that storing `fn(&'a str)` is contravariant in `'a` because a function that can handle `&'short str` (a shorter-lived reference) can certainly be called with `&'long str` (a longer-lived reference). The callback is "less demanding" --- it works with less, so it can accept more.

24. When combining `Producer<'a>` and `Consumer<'a>` in a struct, the variance combination rule applies: covariant + contravariant = invariant. This is the same reason `fn(&'a T) -> &'a T` is invariant in `'a` --- the parameter uses `'a` in both covariant position (return) and contravariant position (argument).

25. For closure variance tests, note that `impl Fn(&'a str)` is NOT the same as `fn(&'a str)` for variance purposes. A closure that captures `&'a str` is covariant in `'a` through its capture, but contravariant through its parameter. Whether the closure is covariant, contravariant, or invariant depends on how `'a` appears in both the captures and the signature. Write tests that distinguish these cases.

26. To test that `ReadOnly<'a, T>` is covariant in `T`, instantiate it with `T = &'long str` and try to pass it where `ReadOnly<'a, &'short str>` is expected (this should work because `&'long str` is a subtype of `&'short str`). For `ReadWrite<'a, T>`, the same substitution should fail because of invariance.

27. For your compile-fail test organization, consider creating a matrix: each row is a type (covariant, contravariant, invariant), each column is a direction (shorten lifetime, lengthen lifetime). That gives you 6 test cases from the matrix alone (3 types x 2 directions), with the compile-fail cases being the forbidden directions for each variance.

28. When documenting the subtyping lattice, clarify the potentially confusing direction: `'long` is a SUBTYPE of `'short` (not the other way around). This is because a `&'long T` can be used anywhere a `&'short T` is expected --- it satisfies a stricter requirement. Subtyping flows from "more capable" to "less capable."

29. For testing variance in the presence of trait bounds, consider: if `T: Display`, does that change the variance of `Container<T>` in `T`? The answer is no --- trait bounds do not affect variance. But adding a bound like `T: 'a` can interact with lifetime variance. Write tests demonstrating that `struct Foo<'a, T: 'a>(&'a T)` has the same variance as `struct Foo<'a, T>(&'a T)` but the bound enables different usages.

30. A powerful exercise: take a real-world type from the standard library (such as `std::cell::Ref<'a, T>`) and determine its variance from first principles by examining its definition. Then verify your analysis with a compile-pass/compile-fail test pair. Do this for at least 5 standard library types.
