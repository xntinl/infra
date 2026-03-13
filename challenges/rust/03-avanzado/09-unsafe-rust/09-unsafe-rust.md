# 9. Unsafe Rust

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-08 (ownership, borrowing, lifetimes, traits, error handling)
- Solid understanding of references, raw pointers conceptually, and memory layout
- Familiarity with C or systems-level memory management helps but is not required

## Learning Objectives

- Identify the five unsafe superpowers and articulate when each is justified
- Analyze soundness invariants that safe wrappers must uphold
- Evaluate whether a given unsafe block is sound or introduces undefined behavior
- Design safe APIs around unsafe internals using encapsulation

## Concepts

Rust's safety guarantees come from the borrow checker, but some operations are provably correct yet impossible to express within its rules. The `unsafe` keyword does not turn off the borrow checker. It unlocks exactly five additional capabilities:

1. **Dereference raw pointers** (`*const T`, `*mut T`)
2. **Call unsafe functions or methods**
3. **Access or modify mutable static variables**
4. **Implement unsafe traits** (`Send`, `Sync`, and custom ones)
5. **Access fields of unions**

Everything else still follows normal Rust rules inside an `unsafe` block.

### Raw Pointers

Raw pointers are created in safe code. Only dereferencing them requires `unsafe`:

```rust
let mut x = 42;
let r1 = &x as *const i32;       // safe: creating a raw pointer
let r2 = &mut x as *mut i32;     // safe: creating a raw pointer

unsafe {
    println!("r1 = {}", *r1);    // unsafe: dereferencing
    *r2 = 99;                     // unsafe: dereferencing + writing
}
```

Raw pointers differ from references in critical ways:
- They can be null
- They can dangle (point to freed memory)
- They can alias mutably (two `*mut T` to the same address)
- They carry no lifetime information

### Unsafe Functions vs Unsafe Blocks

An `unsafe fn` means the caller must uphold invariants the compiler cannot check. An `unsafe` block means the programmer asserts those invariants hold at this call site:

```rust
/// # Safety
/// `ptr` must point to a valid, aligned, initialized `i32`.
unsafe fn read_val(ptr: *const i32) -> i32 {
    *ptr
}

fn safe_wrapper(x: &i32) -> i32 {
    // We know &i32 is always valid, aligned, and initialized.
    unsafe { read_val(x as *const i32) }
}
```

The `# Safety` doc comment is not optional decoration. It is the contract. Without it, reviewers cannot determine if callers uphold invariants.

### Unsafe Traits

A trait is marked `unsafe` when implementing it incorrectly can cause undefined behavior elsewhere. The canonical examples are `Send` and `Sync`:

```rust
unsafe trait TrustMe {
    fn do_thing(&self);
}

struct MyType;

// By writing `unsafe impl`, you promise your implementation
// upholds whatever invariants the trait documents.
unsafe impl TrustMe for MyType {
    fn do_thing(&self) {
        // ...
    }
}
```

`Send` means a type can move between threads safely. `Sync` means `&T` can be shared between threads. If you implement these for a type that is not actually thread-safe, you get data races -- undefined behavior.

### Mutable Statics and Unions

```rust
static mut COUNTER: u64 = 0;

fn increment() {
    // Any access to a mutable static is unsafe because
    // the compiler cannot prove no data race exists.
    unsafe { COUNTER += 1; }
}
```

Prefer `AtomicU64` or `Mutex<u64>` over mutable statics. The only place mutable statics are truly justified is in FFI or `#[no_std]` interrupt handlers.

Unions exist primarily for FFI with C unions:

```rust
#[repr(C)]
union IntOrFloat {
    i: i32,
    f: f32,
}

let u = IntOrFloat { i: 42 };
// Reading the wrong field is UB if the bit pattern is invalid for that type.
unsafe { println!("{}", u.f); }
```

### Wrapping Unsafe in Safe APIs

The real skill of unsafe Rust is not writing `unsafe` blocks -- it is designing the safe API boundary so that no sequence of safe calls can trigger UB. This is the **soundness** requirement.

```rust
pub struct SplitSlice<'a, T> {
    data: &'a mut [T],
}

impl<'a, T> SplitSlice<'a, T> {
    /// Split at index `mid`. Panics if `mid > len`.
    pub fn split_at_mut(&mut self, mid: usize) -> (&mut [T], &mut [T]) {
        let len = self.data.len();
        assert!(mid <= len);
        let ptr = self.data.as_mut_ptr();
        unsafe {
            (
                std::slice::from_raw_parts_mut(ptr, mid),
                std::slice::from_raw_parts_mut(ptr.add(mid), len - mid),
            )
        }
    }
}
```

The `assert!` is critical. Without it, `ptr.add(mid)` could go out of bounds -- UB even if you never dereference it (pointer arithmetic past one-past-the-end is UB).

### Undefined Behavior Examples

These are all UB. Some may appear to "work" on your machine today and break tomorrow:

```rust
// 1. Dangling pointer dereference
let ptr = {
    let x = 42;
    &x as *const i32
}; // x dropped
unsafe { *ptr }; // UB: dangling

// 2. Creating an invalid reference
let ptr: *const i32 = std::ptr::null();
unsafe { &*ptr }; // UB: null reference (references can never be null)

// 3. Data race
static mut X: i32 = 0;
// Two threads writing to X without synchronization: UB

// 4. Breaking aliasing rules
let mut v = vec![1, 2, 3];
let ptr = v.as_ptr();
v.push(4); // may reallocate
unsafe { *ptr }; // UB: ptr may dangle

// 5. Invalid enum discriminant
let b: bool = unsafe { std::mem::transmute(2u8) }; // UB: bool must be 0 or 1
```

### Miri

Miri is an interpreter for Rust's Mid-level Intermediate Representation that detects many forms of UB at runtime:

```bash
rustup +nightly component add miri
cargo +nightly miri test
```

Miri catches: out-of-bounds access, use-after-free, invalid alignment, data races, invalid values, and more. It does not catch all UB (it cannot prove absence of UB), but it is the single best tool for unsafe code validation.

## Exercises

### Exercise 1: Implement a Safe Split

Write a function `split_at_mut` that takes a `&mut [i32]` and an index, returning two non-overlapping mutable slices. You must use `unsafe` internally but expose a safe API.

**Hints:**
- `slice.as_mut_ptr()` gives you a `*mut i32`
- `std::slice::from_raw_parts_mut(ptr, len)` creates a `&mut [T]` from raw parts
- Think carefully about what invariant the caller must not be able to violate

**Cargo.toml:**
```toml
[package]
name = "unsafe-exercises"
edition = "2021"
```

<details>
<summary>Solution</summary>

```rust
fn split_at_mut(slice: &mut [i32], mid: usize) -> (&mut [i32], &mut [i32]) {
    let len = slice.len();
    assert!(mid <= len, "mid ({mid}) > len ({len})");

    let ptr = slice.as_mut_ptr();

    // SAFETY:
    // - `ptr` is valid for `len` elements (it came from a valid slice).
    // - `mid <= len`, so `ptr` is valid for `mid` elements and
    //   `ptr.add(mid)` is valid for `len - mid` elements.
    // - The two slices do not overlap because [0..mid) and [mid..len) are disjoint.
    // - We hold `&mut` to the original slice, so no other references exist.
    unsafe {
        (
            std::slice::from_raw_parts_mut(ptr, mid),
            std::slice::from_raw_parts_mut(ptr.add(mid), len - mid),
        )
    }
}

fn main() {
    let mut data = vec![1, 2, 3, 4, 5];
    let (left, right) = split_at_mut(&mut data, 3);
    left[0] = 10;
    right[0] = 40;
    assert_eq!(left, &[10, 2, 3]);
    assert_eq!(right, &[40, 5]);
    println!("split works: {left:?} | {right:?}");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn split_empty_left() {
        let mut data = vec![1, 2, 3];
        let (left, right) = split_at_mut(&mut data, 0);
        assert!(left.is_empty());
        assert_eq!(right, &[1, 2, 3]);
    }

    #[test]
    #[should_panic]
    fn split_out_of_bounds() {
        let mut data = vec![1, 2];
        split_at_mut(&mut data, 5);
    }
}
```

Run with Miri: `cargo +nightly miri test`
</details>

### Exercise 2: Unsafe Trait for Pinky Promises

Define an `unsafe trait ZeroCopy` that marks types safe to transmute from a byte slice. Implement it for `u32` and `[u8; 4]`. Write a safe `from_bytes` function that uses this trait bound.

**Hints:**
- Not all types are valid for arbitrary byte patterns (`bool`, enums, references are not)
- Your safety contract: implementors must be valid for any bit pattern of the right size, have no padding, and have alignment 1 or use `#[repr(C)]`/`#[repr(transparent)]`
- `std::mem::size_of::<T>()` and `std::ptr::read_unaligned` are useful

<details>
<summary>Solution</summary>

```rust
use std::mem;

/// # Safety
/// Implementing types must:
/// 1. Be valid for every possible bit pattern of size `size_of::<Self>()`
/// 2. Have no padding bytes
/// 3. Have no references or pointer-dependent invariants
unsafe trait ZeroCopy: Sized {}

unsafe impl ZeroCopy for u8 {}
unsafe impl ZeroCopy for u32 {}
unsafe impl ZeroCopy for [u8; 4] {}
// NOTE: Do NOT implement for bool, char, enums, or types with references.

fn from_bytes<T: ZeroCopy>(bytes: &[u8]) -> Option<T> {
    if bytes.len() < mem::size_of::<T>() {
        return None;
    }

    // SAFETY: T implements ZeroCopy, so any bit pattern is valid.
    // We use read_unaligned because bytes may not be properly aligned for T.
    let val = unsafe { std::ptr::read_unaligned(bytes.as_ptr() as *const T) };
    Some(val)
}

fn main() {
    let bytes = [0x78, 0x56, 0x34, 0x12];
    let val: u32 = from_bytes(&bytes).unwrap();
    // On little-endian: 0x12345678
    println!("u32 from bytes: {val:#x}");

    let arr: [u8; 4] = from_bytes(&bytes).unwrap();
    println!("array from bytes: {arr:?}");

    let too_short: Option<u32> = from_bytes(&[1, 2]);
    assert!(too_short.is_none());
}
```

This pattern appears in production crates like `bytemuck` and `zerocopy`. Compare your trait contract to `bytemuck::Pod` -- the constraints are essentially identical.
</details>

### Exercise 3: Find the UB

The following code compiles and runs "successfully" on most machines. Identify all instances of undefined behavior, explain why each is UB, and fix them.

```rust
fn main() {
    // Snippet A
    let mut v = vec![1, 2, 3];
    let first = &v[0] as *const i32;
    v.push(4);
    unsafe { println!("first = {}", *first); }

    // Snippet B
    let x: u8 = unsafe { std::mem::transmute(300u16) };

    // Snippet C
    let reference: &i32 = unsafe { &*(0x1 as *const i32) };
}
```

**Hints:**
- Snippet A: what does `Vec::push` do when capacity is exceeded?
- Snippet B: what happens to the bits?
- Snippet C: is `0x1` a valid, aligned, dereferenceable address?

Write corrected versions and verify with `cargo +nightly miri test`.

## Common Mistakes

1. **Assuming "it runs fine" means no UB.** UB means the compiler may assume it never happens. It can optimize based on that assumption. Your code might work today and break with the next compiler release.

2. **Too-large unsafe blocks.** Keep unsafe blocks minimal. Each one is a contract you must manually verify. A 50-line unsafe block is 50 lines of audit surface.

3. **Missing `# Safety` documentation.** If your function is `unsafe`, you must document what invariants callers must uphold. "Be careful" is not a safety contract.

4. **Using `transmute` when a safer alternative exists.** `as` casts, `to_ne_bytes`, `from_ne_bytes`, `bytemuck::cast` -- reach for these first.

5. **Forgetting alignment.** `read_unaligned` / `write_unaligned` exist for a reason. Creating a `&T` to an unaligned address is instant UB, even if you never read through it.

## Verification

- All exercises should pass `cargo test`
- All exercises should pass `cargo +nightly miri test` with zero errors
- Run `cargo clippy` -- it catches some unsound patterns

## Summary

Unsafe Rust is not an escape hatch from thinking about safety. It is a contract: the programmer takes responsibility for invariants the compiler normally enforces. The goal is always to minimize the surface area of unsafe code and wrap it behind safe APIs whose type signatures make misuse impossible.

## What's Next

Exercise 10 explores advanced traits -- the type-system features that let you build powerful safe abstractions so you need unsafe less often.

## Resources

- [The Rustonomicon](https://doc.rust-lang.org/nomicon/) -- the definitive guide to unsafe Rust
- [Miri](https://github.com/rust-lang/miri) -- UB detection tool
- [bytemuck](https://docs.rs/bytemuck) -- production-grade zero-copy crate
- [UCG (Unsafe Code Guidelines)](https://rust-lang.github.io/unsafe-code-guidelines/) -- ongoing specification work
- [Too Many Linked Lists](https://rust-unofficial.github.io/too-many-lists/) -- unsafe in practice
