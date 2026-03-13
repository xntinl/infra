# 12. FFI and C Interop

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-11 (ownership, unsafe, advanced lifetimes)
- Basic understanding of C: pointers, null termination, manual memory management
- Familiarity with `unsafe` blocks and the five unsafe superpowers

## Learning Objectives

- Call C functions from Rust using `extern "C"` and link against C libraries
- Expose Rust functions to C with `#[no_mangle]` and `extern "C"`
- Handle string conversion between Rust (`str`/`String`) and C (`char*`) via `CStr`/`CString`
- Design safe Rust wrappers around unsafe FFI boundaries
- Configure `build.rs` to compile and link C code

## Concepts

### Calling C from Rust

Declare external C functions in an `extern "C"` block. Each call is `unsafe` because the compiler cannot verify the C side upholds Rust's safety invariants:

```rust
// Declare the C function signature
extern "C" {
    fn abs(x: i32) -> i32;
    fn strlen(s: *const std::ffi::c_char) -> usize;
}

fn main() {
    // Every FFI call is unsafe -- the compiler trusts your declaration
    let result = unsafe { abs(-42) };
    assert_eq!(result, 42);
}
```

The `"C"` specifies the calling convention (the C ABI). Other options include `"system"` (stdcall on Windows, C elsewhere) and `"stdcall"`.

### Exposing Rust to C

Mark functions with `extern "C"` and `#[no_mangle]` so C code can call them:

```rust
#[no_mangle]
pub extern "C" fn rust_add(a: i32, b: i32) -> i32 {
    a + b
}
```

`#[no_mangle]` prevents the Rust compiler from mangling the symbol name. Without it, the linker cannot find the function.

From C:
```c
// header: rust_lib.h
int32_t rust_add(int32_t a, int32_t b);
```

### repr(C)

Rust structs have no guaranteed memory layout. `#[repr(C)]` forces C-compatible layout (fields in declaration order, C alignment rules):

```rust
#[repr(C)]
pub struct Point {
    pub x: f64,
    pub y: f64,
}

// This struct can be passed to/from C directly.
// Without repr(C), Rust may reorder fields for optimization.
```

Type mapping between Rust and C:

| Rust | C | Notes |
|---|---|---|
| `i8` / `u8` | `int8_t` / `uint8_t` | Exact match |
| `i32` / `u32` | `int32_t` / `uint32_t` | Use fixed-width types |
| `f32` / `f64` | `float` / `double` | Exact match |
| `bool` | `_Bool` / `bool` | C99+ required |
| `*const T` / `*mut T` | `const T*` / `T*` | Pointer types |
| `std::ffi::c_char` | `char` | Platform-dependent signedness |
| `()` | `void` | Return type only |
| `Option<&T>` | `T*` | `None` = null (niche optimization) |

### CStr and CString

C strings are null-terminated byte arrays. Rust strings are length-prefixed UTF-8 with no null terminator. You must convert explicitly:

```rust
use std::ffi::{CStr, CString};
use std::os::raw::c_char;

// Rust -> C: create a CString (owned, null-terminated)
fn rust_to_c() {
    let rust_str = "hello";
    let c_string = CString::new(rust_str).expect("CString::new failed");
    let ptr: *const c_char = c_string.as_ptr();

    // IMPORTANT: c_string must outlive the pointer.
    // Dropping c_string invalidates ptr.
    unsafe { some_c_function(ptr); }
}

// C -> Rust: wrap a raw pointer in CStr (borrowed, not owned)
unsafe fn c_to_rust(ptr: *const c_char) -> &'static str {
    // SAFETY: ptr must be non-null, point to a valid null-terminated string,
    // and the string must live for 'static.
    let c_str = unsafe { CStr::from_ptr(ptr) };
    c_str.to_str().expect("invalid UTF-8")
}
```

**`CString::new` will fail if the input contains interior null bytes.** This is a common source of bugs.

### Null Pointers

C uses null pointers extensively. Rust references can never be null, but raw pointers can:

```rust
use std::ptr;

#[no_mangle]
pub extern "C" fn process(data: *const u8, len: usize) -> i32 {
    if data.is_null() {
        return -1; // Error code
    }

    // SAFETY: caller guarantees data points to `len` valid bytes
    let slice = unsafe { std::slice::from_raw_parts(data, len) };
    slice.iter().map(|&b| b as i32).sum()
}
```

Rust guarantees that `Option<&T>` and `Option<Box<T>>` have the same size as a raw pointer, with `None` represented as null. This makes `Option` a zero-cost nullable pointer at the FFI boundary:

```rust
#[no_mangle]
pub extern "C" fn maybe_get(idx: usize) -> *const Point {
    // Internally use Option, convert to raw pointer for C
    let result: Option<&Point> = lookup(idx);
    match result {
        Some(p) => p as *const Point,
        None => std::ptr::null(),
    }
}
```

### Error Handling Across FFI

Rust panics and C do not mix. A panic that unwinds across an `extern "C"` boundary is undefined behavior. Strategies:

```rust
use std::ffi::{c_char, CStr, CString};

// Strategy 1: Return error codes
#[no_mangle]
pub extern "C" fn divide(a: f64, b: f64, result: *mut f64) -> i32 {
    if b == 0.0 {
        return -1; // error
    }
    if result.is_null() {
        return -2; // null pointer
    }
    unsafe { *result = a / b; }
    0 // success
}

// Strategy 2: Thread-local error message
use std::cell::RefCell;

thread_local! {
    static LAST_ERROR: RefCell<Option<CString>> = RefCell::new(None);
}

fn set_last_error(msg: &str) {
    LAST_ERROR.with(|e| {
        *e.borrow_mut() = CString::new(msg).ok();
    });
}

#[no_mangle]
pub extern "C" fn get_last_error() -> *const c_char {
    LAST_ERROR.with(|e| {
        match e.borrow().as_ref() {
            Some(s) => s.as_ptr(),
            None => std::ptr::null(),
        }
    })
}

// Strategy 3: Catch panics at the boundary
#[no_mangle]
pub extern "C" fn safe_entry_point(input: i32) -> i32 {
    let result = std::panic::catch_unwind(|| {
        // Rust code that might panic
        if input < 0 {
            panic!("negative input");
        }
        input * 2
    });

    match result {
        Ok(val) => val,
        Err(_) => {
            set_last_error("panic occurred in Rust code");
            -1
        }
    }
}
```

### build.rs for Linking

`build.rs` is a build script that runs before compilation. Use it to compile C code and link it:

```rust
// build.rs
fn main() {
    // Compile C source files
    cc::Build::new()
        .file("src/native/math_utils.c")
        .compile("math_utils");

    // Link a system library
    println!("cargo:rustc-link-lib=z"); // links libz

    // Tell Cargo to re-run if the C file changes
    println!("cargo:rerun-if-changed=src/native/math_utils.c");
}
```

```toml
# Cargo.toml
[build-dependencies]
cc = "1"
```

### bindgen and cbindgen

**bindgen**: generates Rust FFI bindings from C headers automatically:
```toml
[build-dependencies]
bindgen = "0.71"
```

```rust
// build.rs
fn main() {
    let bindings = bindgen::Builder::default()
        .header("wrapper.h")
        .generate()
        .expect("Unable to generate bindings");

    let out_path = std::path::PathBuf::from(std::env::var("OUT_DIR").unwrap());
    bindings
        .write_to_file(out_path.join("bindings.rs"))
        .expect("Couldn't write bindings");
}
```

**cbindgen**: generates C headers from Rust code:
```bash
cargo install cbindgen
cbindgen --lang c --output include/mylib.h
```

## Exercises

### Exercise 1: Wrap a C Library

Write a safe Rust wrapper around C's `qsort` function. Your wrapper should:

1. Declare `qsort` via `extern "C"`
2. Accept a `&mut [i32]` and sort it
3. Handle the unsafe C callback mechanism (comparison function pointer)
4. Expose a completely safe public API

**Hints:**
- `qsort` signature: `void qsort(void *base, size_t nmemb, size_t size, int (*compar)(const void *, const void *))`
- The comparison callback receives `*const c_void` pointers that you cast to `*const i32`
- Your safe wrapper should look like: `fn sort_ints(data: &mut [i32])`

**Cargo.toml:**
```toml
[package]
name = "ffi-exercises"
edition = "2021"
```

<details>
<summary>Solution</summary>

```rust
use std::ffi::c_void;
use std::cmp::Ordering;

extern "C" {
    fn qsort(
        base: *mut c_void,
        nmemb: usize,
        size: usize,
        compar: extern "C" fn(*const c_void, *const c_void) -> i32,
    );
}

// The comparison function must be extern "C" to match the expected ABI.
extern "C" fn compare_i32(a: *const c_void, b: *const c_void) -> i32 {
    // SAFETY: qsort calls this with pointers into the original array,
    // which contains i32 values. The pointers are valid and aligned
    // because the array itself is valid.
    let a = unsafe { *(a as *const i32) };
    let b = unsafe { *(b as *const i32) };
    match a.cmp(&b) {
        Ordering::Less => -1,
        Ordering::Equal => 0,
        Ordering::Greater => 1,
    }
}

/// Sort a slice of i32 using C's qsort.
/// This is a safe wrapper: no unsafe code is exposed to the caller.
pub fn sort_ints(data: &mut [i32]) {
    if data.is_empty() {
        return;
    }
    // SAFETY:
    // - data.as_mut_ptr() is valid for data.len() elements of size_of::<i32>()
    // - compare_i32 follows the qsort contract (returns <0, 0, >0)
    // - qsort does not hold pointers past its return
    // - No panics cross the FFI boundary (compare_i32 cannot panic)
    unsafe {
        qsort(
            data.as_mut_ptr() as *mut c_void,
            data.len(),
            std::mem::size_of::<i32>(),
            compare_i32,
        );
    }
}

fn main() {
    let mut numbers = vec![5, 2, 8, 1, 9, 3];
    sort_ints(&mut numbers);
    println!("sorted: {numbers:?}");
    assert_eq!(numbers, vec![1, 2, 3, 5, 8, 9]);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sorts_correctly() {
        let mut data = vec![3, 1, 4, 1, 5, 9, 2, 6];
        sort_ints(&mut data);
        assert_eq!(data, vec![1, 1, 2, 3, 4, 5, 6, 9]);
    }

    #[test]
    fn empty_slice() {
        let mut data: Vec<i32> = vec![];
        sort_ints(&mut data); // must not crash
        assert!(data.is_empty());
    }

    #[test]
    fn single_element() {
        let mut data = vec![42];
        sort_ints(&mut data);
        assert_eq!(data, vec![42]);
    }

    #[test]
    fn already_sorted() {
        let mut data = vec![1, 2, 3, 4, 5];
        sort_ints(&mut data);
        assert_eq!(data, vec![1, 2, 3, 4, 5]);
    }

    #[test]
    fn negative_numbers() {
        let mut data = vec![-3, -1, -4, 0, 2];
        sort_ints(&mut data);
        assert_eq!(data, vec![-4, -3, -1, 0, 2]);
    }
}
```
</details>

### Exercise 2: Expose Rust to C (Key-Value Store)

Build a simple in-memory key-value store in Rust, then expose it to C via FFI. The C API should be:

```c
typedef struct KvStore KvStore;

KvStore* kv_store_new(void);
void kv_store_free(KvStore* store);
int kv_store_set(KvStore* store, const char* key, const char* value);
const char* kv_store_get(KvStore* store, const char* key);
void kv_store_string_free(char* s);
```

**Hints:**
- Use `Box::into_raw` to hand ownership to C, `Box::from_raw` to reclaim it
- `kv_store_get` must return a `CString` that C can free with `kv_store_string_free`
- Null-check every pointer parameter
- Catch panics at every entry point

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;
use std::ffi::{c_char, CStr, CString};

pub struct KvStore {
    data: HashMap<String, String>,
}

impl KvStore {
    fn new() -> Self {
        Self { data: HashMap::new() }
    }

    fn set(&mut self, key: String, value: String) {
        self.data.insert(key, value);
    }

    fn get(&self, key: &str) -> Option<&str> {
        self.data.get(key).map(|s| s.as_str())
    }
}

// --- FFI boundary ---

/// Create a new key-value store. Caller must free with kv_store_free.
#[no_mangle]
pub extern "C" fn kv_store_new() -> *mut KvStore {
    let store = Box::new(KvStore::new());
    Box::into_raw(store) // Ownership transferred to C
}

/// Free a key-value store. Passing null is a no-op.
#[no_mangle]
pub extern "C" fn kv_store_free(store: *mut KvStore) {
    if store.is_null() {
        return;
    }
    // SAFETY: store was created by kv_store_new via Box::into_raw.
    // This takes back ownership and drops it.
    unsafe {
        drop(Box::from_raw(store));
    }
}

/// Set a key-value pair. Returns 0 on success, -1 on error.
#[no_mangle]
pub extern "C" fn kv_store_set(
    store: *mut KvStore,
    key: *const c_char,
    value: *const c_char,
) -> i32 {
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        if store.is_null() || key.is_null() || value.is_null() {
            return -1;
        }

        // SAFETY: caller guarantees valid null-terminated strings
        let key_str = unsafe { CStr::from_ptr(key) };
        let value_str = unsafe { CStr::from_ptr(value) };

        let key_str = match key_str.to_str() {
            Ok(s) => s.to_string(),
            Err(_) => return -1,
        };
        let value_str = match value_str.to_str() {
            Ok(s) => s.to_string(),
            Err(_) => return -1,
        };

        let store = unsafe { &mut *store };
        store.set(key_str, value_str);
        0
    }));

    result.unwrap_or(-1)
}

/// Get a value by key. Returns null if not found.
/// Caller must free the returned string with kv_store_string_free.
#[no_mangle]
pub extern "C" fn kv_store_get(store: *const KvStore, key: *const c_char) -> *mut c_char {
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        if store.is_null() || key.is_null() {
            return std::ptr::null_mut();
        }

        let key_str = unsafe { CStr::from_ptr(key) };
        let key_str = match key_str.to_str() {
            Ok(s) => s,
            Err(_) => return std::ptr::null_mut(),
        };

        let store = unsafe { &*store };
        match store.get(key_str) {
            Some(val) => {
                // Allocate a new CString for the caller to own
                match CString::new(val) {
                    Ok(c) => c.into_raw(), // caller must free
                    Err(_) => std::ptr::null_mut(),
                }
            }
            None => std::ptr::null_mut(),
        }
    }));

    result.unwrap_or(std::ptr::null_mut())
}

/// Free a string returned by kv_store_get. Passing null is a no-op.
#[no_mangle]
pub extern "C" fn kv_store_string_free(s: *mut c_char) {
    if s.is_null() {
        return;
    }
    // SAFETY: s was created by CString::into_raw in kv_store_get
    unsafe {
        drop(CString::from_raw(s));
    }
}

fn main() {
    // Simulate C calling our API
    let store = kv_store_new();

    let key = CString::new("host").unwrap();
    let val = CString::new("localhost").unwrap();
    let rc = kv_store_set(store, key.as_ptr(), val.as_ptr());
    assert_eq!(rc, 0);

    let result = kv_store_get(store, key.as_ptr());
    if !result.is_null() {
        let s = unsafe { CStr::from_ptr(result) };
        println!("got: {}", s.to_str().unwrap());
        kv_store_string_free(result);
    }

    // Null key returns -1
    let rc = kv_store_set(store, std::ptr::null(), val.as_ptr());
    assert_eq!(rc, -1);

    kv_store_free(store);
    println!("all operations completed");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip() {
        let store = kv_store_new();
        let k = CString::new("k").unwrap();
        let v = CString::new("v").unwrap();

        assert_eq!(kv_store_set(store, k.as_ptr(), v.as_ptr()), 0);

        let result = kv_store_get(store, k.as_ptr());
        assert!(!result.is_null());
        let got = unsafe { CStr::from_ptr(result) }.to_str().unwrap();
        assert_eq!(got, "v");
        kv_store_string_free(result);

        kv_store_free(store);
    }

    #[test]
    fn get_missing_key() {
        let store = kv_store_new();
        let k = CString::new("missing").unwrap();
        let result = kv_store_get(store, k.as_ptr());
        assert!(result.is_null());
        kv_store_free(store);
    }

    #[test]
    fn null_safety() {
        assert_eq!(kv_store_set(std::ptr::null_mut(), std::ptr::null(), std::ptr::null()), -1);
        assert!(kv_store_get(std::ptr::null(), std::ptr::null()).is_null());
        kv_store_free(std::ptr::null_mut()); // must not crash
        kv_store_string_free(std::ptr::null_mut()); // must not crash
    }
}
```

**Design decisions:**

| Decision | Rationale |
|---|---|
| `Box::into_raw` / `Box::from_raw` | Transfers ownership across FFI cleanly |
| Separate `kv_store_string_free` | Caller must use our allocator to free our allocations |
| `catch_unwind` at every entry | Panics across FFI are UB |
| Null checks on every pointer | C passes null routinely; crashing is unacceptable |
| Return error codes, not exceptions | C has no exception mechanism |

</details>

### Exercise 3: Build Script with C Compilation

Create a project that:
1. Has a C file `src/native/hasher.c` implementing a simple FNV-1a hash function
2. Uses `build.rs` with the `cc` crate to compile it
3. Declares the function in Rust and wraps it in a safe API
4. Compares the result against a pure Rust implementation

**Hints:**
- FNV-1a: `hash = offset_basis; for each byte: hash ^= byte; hash *= prime;`
- The `cc` crate in `build.rs` compiles `.c` files and links them automatically
- Use `cargo:rerun-if-changed` to avoid unnecessary rebuilds

<details>
<summary>Solution</summary>

**Cargo.toml:**
```toml
[package]
name = "ffi-build-script"
edition = "2021"

[build-dependencies]
cc = "1"
```

**src/native/hasher.c:**
```c
#include <stdint.h>
#include <stddef.h>

#define FNV_OFFSET 2166136261u
#define FNV_PRIME  16777619u

uint32_t fnv1a_hash(const uint8_t *data, size_t len) {
    uint32_t hash = FNV_OFFSET;
    for (size_t i = 0; i < len; i++) {
        hash ^= data[i];
        hash *= FNV_PRIME;
    }
    return hash;
}
```

**build.rs:**
```rust
fn main() {
    cc::Build::new()
        .file("src/native/hasher.c")
        .compile("hasher");

    println!("cargo:rerun-if-changed=src/native/hasher.c");
    println!("cargo:rerun-if-changed=build.rs");
}
```

**src/main.rs:**
```rust
extern "C" {
    fn fnv1a_hash(data: *const u8, len: usize) -> u32;
}

/// Safe wrapper around the C FNV-1a implementation.
pub fn c_fnv1a(data: &[u8]) -> u32 {
    if data.is_empty() {
        // FNV offset basis for empty input
        return 2166136261;
    }
    // SAFETY: data.as_ptr() is valid for data.len() bytes
    unsafe { fnv1a_hash(data.as_ptr(), data.len()) }
}

/// Pure Rust implementation for comparison.
pub fn rust_fnv1a(data: &[u8]) -> u32 {
    const OFFSET: u32 = 2166136261;
    const PRIME: u32 = 16777619;

    let mut hash = OFFSET;
    for &byte in data {
        hash ^= byte as u32;
        hash = hash.wrapping_mul(PRIME);
    }
    hash
}

fn main() {
    let inputs = ["hello", "world", "", "The quick brown fox"];

    for input in &inputs {
        let c_result = c_fnv1a(input.as_bytes());
        let rust_result = rust_fnv1a(input.as_bytes());
        println!(
            "{:20} -> C: {:#010x}, Rust: {:#010x}, match: {}",
            input,
            c_result,
            rust_result,
            c_result == rust_result
        );
        assert_eq!(c_result, rust_result, "mismatch for {input:?}");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn c_and_rust_agree() {
        let cases = [
            b"test".as_slice(),
            b"",
            b"\x00",
            b"a]ong string with various bytes \xff\x00\x01",
        ];

        for data in &cases {
            assert_eq!(
                c_fnv1a(data),
                rust_fnv1a(data),
                "mismatch for {data:?}"
            );
        }
    }

    #[test]
    fn known_value() {
        // FNV-1a of "hello" is a well-known value
        let hash = rust_fnv1a(b"hello");
        assert_eq!(hash, 0x4f9f2cab);
    }
}
```

**Project structure:**
```
ffi-build-script/
  Cargo.toml
  build.rs
  src/
    main.rs
    native/
      hasher.c
```
</details>

## Common Mistakes

1. **Dropping `CString` while C still holds the pointer.** The pointer from `CString::as_ptr()` is invalidated when the `CString` is dropped. Keep the `CString` alive or use `into_raw` and document the ownership transfer.

2. **Not null-checking FFI pointers.** C code passes null pointers routinely. Every FFI entry point must check before dereferencing.

3. **Panicking across FFI boundaries.** Unwinding through `extern "C"` is UB. Use `catch_unwind` at every public entry point.

4. **Forgetting `#[repr(C)]`.** Without it, Rust may reorder struct fields. The C side reads garbage.

5. **Mixing allocators.** If Rust allocates memory (via `Box`, `CString`, `Vec`), Rust must free it. Do not call `free()` on Rust-allocated memory. Provide explicit deallocation functions.

## Verification

- All exercises should pass `cargo test`
- Exercise 3 requires the `cc` crate: `cargo build` compiles both C and Rust
- Run under Valgrind or AddressSanitizer to verify no memory leaks at the FFI boundary
- `cargo +nightly miri test` works for exercises 1 and 2 (Miri does not support FFI calls to real C, but the pure-Rust paths are checked)

## Summary

FFI is where Rust's safety guarantees meet the outside world. The compiler cannot help you across the boundary -- you must manually uphold every invariant. The key discipline is: minimize the unsafe surface, null-check everything, catch panics, document ownership transfers, and provide safe wrappers that make misuse impossible from the Rust side.

## What's Next

Exercise 13 covers serde and serialization -- turning Rust types into bytes and back, which is the safe-Rust equivalent of the data transformation problems FFI forces you to solve with raw pointers.

## Resources

- [The Rustonomicon: FFI](https://doc.rust-lang.org/nomicon/ffi.html)
- [Rust FFI Omnibus](https://jakegoulding.com/rust-ffi-omnibus/)
- [cc crate](https://docs.rs/cc)
- [bindgen](https://rust-lang.github.io/rust-bindgen/)
- [cbindgen](https://github.com/mozilla/cbindgen)
- [CXX crate](https://cxx.rs/) -- safe C++/Rust interop (alternative to raw FFI)
