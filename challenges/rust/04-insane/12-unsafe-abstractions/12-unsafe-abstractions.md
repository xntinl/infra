# 12. Unsafe Abstractions
**Difficulty**: Insane

## The Challenge

Rust's `unsafe` keyword is not an escape hatch from correctness -- it is a contract between the programmer and the compiler. When you write `unsafe`, you are telling the compiler "I have manually verified that every invariant you cannot check is upheld." The goal of this exercise is to build three non-trivial data structures that use `unsafe` internally but expose completely safe public APIs, and to prove their correctness under Miri's stacked borrows model.

You will implement three components. First, a custom `MyVec<T>` that manages its own heap allocation using `std::alloc`, supports push, pop, indexing, iteration, and must correctly handle zero-sized types (ZSTs), alignment requirements, and drop semantics. Second, a self-referential struct `SelfRef` that holds an owned `String` and a raw pointer into that string's buffer, demonstrating the pin-based pattern for self-referential data without any external crate. Third, a fixed-size memory pool allocator `Pool<T>` that pre-allocates a slab of N objects and hands out safe references with lifetime guarantees, supporting both exclusive and shared borrowing patterns.

Every single one of these must pass Miri with `-Zmiri-tag-raw-pointers` and `-Zmiri-check-number-validity`. This is not optional. If Miri reports undefined behavior, the implementation is wrong regardless of whether it "works" on your machine. You will also write property-based tests using `proptest` to fuzz operations against the standard library equivalents and verify behavioral equivalence.

## Acceptance Criteria

### MyVec<T>

- [ ] Implements a generic `MyVec<T>` using `std::alloc::alloc`, `realloc`, and `dealloc` directly -- no `Vec`, `Box`, or `RawVec` anywhere in the implementation
- [ ] Supports `new()`, `with_capacity(cap)`, `push(val)`, `pop() -> Option<T>`, `len()`, `capacity()`, `is_empty()`
- [ ] Implements `Index<usize>` and `IndexMut<usize>` with proper bounds checking and panics on out-of-bounds
- [ ] Implements `Deref<Target = [T]>` and `DerefMut` so all slice methods are available
- [ ] Growth strategy doubles capacity (starting from 1 if currently 0), matching `Vec`'s amortized O(1) push
- [ ] Correctly handles zero-sized types: no allocation occurs, capacity is `usize::MAX`, pointer is `NonNull::dangling()`
- [ ] Correctly handles types with non-trivial alignment (e.g., `#[repr(align(64))]` structs) by passing correct `Layout` to allocator
- [ ] `Drop` implementation drops all contained elements in order, then deallocates the buffer (but does not deallocate if capacity is 0 or T is ZST)
- [ ] Implements `IntoIterator` for `MyVec<T>`, `&MyVec<T>`, and `&mut MyVec<T>`
- [ ] The owned `IntoIter` takes ownership of the allocation, drops remaining elements when the iterator is dropped, and deallocates
- [ ] Implements `FromIterator<T>` so `collect()` works
- [ ] Implements `Clone` where `T: Clone`, `Debug` where `T: Debug`, `PartialEq` where `T: PartialEq`
- [ ] All operations are panic-safe: if `T::clone` panics during `Clone`, already-cloned elements are dropped and the buffer is deallocated
- [ ] Passes `cargo +nightly miri test` with no undefined behavior detected

### SelfRef

- [ ] Defines a struct `SelfRef` that owns a `String` and holds a `*const str` pointing into that string
- [ ] The pointer is initialized after construction and is valid for the lifetime of the struct
- [ ] Implements a `new(s: String) -> Pin<Box<SelfRef>>` constructor that allocates, writes the string, then sets the self-pointer
- [ ] Implements a `get_ref(&self) -> &str` method that dereferences the raw pointer safely (the struct being pinned guarantees the pointer remains valid)
- [ ] The struct is `!Unpin` (enforced via `PhantomPinned`)
- [ ] Demonstrates that moving the struct after pinning is a compile-time error (include a doc-test or compile-fail test)
- [ ] `Drop` implementation is correct and does not double-free (only the `String` needs dropping, the pointer is just a view)
- [ ] Includes a more complex variant `SelfRefVec` that holds a `Vec<u8>` and multiple `*const [u8]` slices pointing into non-overlapping regions of the buffer
- [ ] Passes Miri with stacked borrows checking enabled

### Pool<T>

- [ ] Implements a fixed-capacity pool `Pool<T, const N: usize>` that pre-allocates storage for N items of type T
- [ ] Uses `MaybeUninit<T>` array for storage, tracking which slots are occupied via a bitset or free list
- [ ] `alloc(&self) -> Option<PoolRef<'_, T>>` returns a smart pointer that derefs to `&mut T` and returns the slot on drop
- [ ] `PoolRef` implements `Deref<Target = T>` and `DerefMut`
- [ ] The pool itself only requires `&self` for allocation (interior mutability via `UnsafeCell`)
- [ ] Maximum of N simultaneous allocations; `alloc()` returns `None` when full
- [ ] Dropping a `PoolRef` marks the slot as free and drops the contained `T`
- [ ] The pool is `!Sync` (because `PoolRef` hands out `&mut T` and the bookkeeping uses `Cell`/`UnsafeCell` without atomic operations)
- [ ] Implements a `drain()` method that drops all currently allocated items and resets the pool
- [ ] No memory leaks: dropping the `Pool` drops all live items (even if their `PoolRef`s were `mem::forget`-ed -- document this limitation if not achievable, as it requires leak-tracking)
- [ ] Passes Miri

### General

- [ ] Property-based tests using `proptest` that generate random sequences of operations (push, pop, index, iterate) on `MyVec<T>` and compare results against `std::vec::Vec<T>`
- [ ] At least 30 unit tests across all three structures
- [ ] Benchmark comparing `MyVec<u64>::push` throughput against `Vec<u64>::push` using `criterion` (should be within 2x)
- [ ] All code compiles on stable Rust (except Miri tests which require nightly)
- [ ] No external crates in the core implementation (only `proptest` and `criterion` in dev-dependencies)
- [ ] Documented with `///` doc comments explaining every `unsafe` block's safety justification

## Starting Points

- Study the `RawVec` implementation in `library/alloc/src/raw_vec.rs` in the Rust repository -- this is the actual backing allocation type for `Vec` and understanding its growth strategy and ZST handling is essential
- Read `library/alloc/src/vec/mod.rs` in the Rust repository for how `Vec` implements `Drop`, `IntoIter`, and panic safety during operations like `clone`
- Study the Rustonomicon chapters on "Implementing Vec" (https://doc.rust-lang.org/nomicon/vec/vec.html) which walks through a correct implementation step by step
- Read the `Pin` module documentation in `core::pin` and the associated RFC 2349 for understanding why self-referential structs need pinning
- Study the `bumpalo` crate's source code (specifically `src/lib.rs`) for how arena allocators manage `MaybeUninit` memory and hand out references
- Read the Miri documentation on Stacked Borrows (https://github.com/rust-lang/miri) to understand what operations trigger undefined behavior
- Study the `typed-arena` crate source for a simpler pool-like allocator pattern
- Read "Leakpocalypse" blog posts about why `mem::forget` is safe and how it affects pool-based designs

## Hints

1. For `MyVec`, start with the `NonNull<T>` pointer type rather than `*mut T`. `NonNull` is covariant and guarantees non-null, which makes the struct more correct by construction. Your struct fields should be `ptr: NonNull<T>`, `len: usize`, `cap: usize`.

2. The ZST case is special throughout. When `size_of::<T>() == 0`, you must never call `alloc`, `realloc`, or `dealloc`. Use `NonNull::dangling()` as the pointer and treat capacity as `usize::MAX`. Guard every allocation path with `if size_of::<T>() != 0`.

3. When computing `Layout` for allocation, use `Layout::array::<T>(cap).unwrap()`. This handles both alignment and overflow checking. Never compute the size manually with multiplication -- it can overflow.

4. For panic safety during `clone`, consider using a guard struct pattern: create a struct that holds the partially-initialized `MyVec` and implements `Drop` to clean up. If a panic occurs during cloning elements, the guard's drop runs and frees everything.

5. The `IntoIter` struct should own the allocation (take the `ptr` and `cap` from `MyVec`, set `MyVec`'s cap to 0 so its `Drop` is a no-op). Track iteration with two pointers: `start` and `end`. Advance `start` forward for `next()` and `end` backward for `next_back()`.

6. For `SelfRef`, the construction order matters critically. You must: (a) allocate the `Box`, (b) write the `String` field, (c) compute the pointer from the string's buffer, (d) write the pointer field. Use `addr_of_mut!` to write fields of the partially-initialized struct, or initialize with a dummy pointer first and then fix it up.

7. The `Pin` guarantee only matters if your type is `!Unpin`. Add `_pin: PhantomPinned` as a field. This prevents callers from getting `&mut SelfRef` through the pin, which would let them `mem::swap` the struct and invalidate the pointer.

8. For `Pool`, the key insight is that `UnsafeCell<MaybeUninit<T>>` lets you get a `*mut T` from `&self`. Combined with a `Cell<u64>` bitset (for N <= 64) tracking which slots are alive, you can implement allocation with only shared references.

9. The `PoolRef` must borrow the `Pool` to prevent the pool from being dropped while references exist. Its type signature should be something like `PoolRef<'a, T> { pool: &'a Pool<T, N>, slot: usize }`. The `Drop` for `PoolRef` calls `pool.free(slot)`.

10. For Miri testing, add a `.cargo/config.toml` with `[target.'cfg(miri)'] runner = "cargo miri"` or just run `cargo +nightly miri test`. Common Miri failures: creating references to uninitialized memory (use `MaybeUninit::assume_init_ref` only after writing), pointer provenance violations (don't cast integers to pointers), and stacked borrows violations (don't create `&mut` while `&` exists).

11. For proptest, create a strategy that generates `enum Op { Push(T), Pop, Index(usize), Len }` and then execute the same sequence on both your `MyVec` and `std::vec::Vec`, asserting equality at every step.

12. The hardest part of the entire exercise is getting `IntoIter` right when `T` has a non-trivial `Drop`. If the iterator is dropped before being fully consumed, you must drop the remaining elements. Handle this in `IntoIter::drop` by iterating from `start` to `end` and calling `ptr::drop_in_place` on each element. Then deallocate the buffer using the original `ptr` and `cap` (not the potentially-advanced `start` pointer).

13. For the `SelfRefVec` variant, pre-compute all slice boundaries before creating any pointers. Store the boundaries as `(offset, len)` pairs alongside the raw pointers, so you can validate in debug builds that the slices are still within bounds.

14. Watch out for the `noalias` requirement on `&mut T`. When `PoolRef` hands out `&mut T`, no other reference to that `T` can exist. This means your free-list or bitset operations must not touch the slot's memory -- only the bookkeeping metadata.

15. To test panic safety, use a custom type `PanicOnClone { should_panic: bool }` that panics during `Clone::clone` when the flag is set. Push several of these into `MyVec`, set one to panic, then call `clone()` on the vec inside `std::panic::catch_unwind`. After the panic, verify no leaks occurred (use a global `AtomicUsize` counter incremented on `new` and decremented on `drop`).
