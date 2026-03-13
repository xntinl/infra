# 19. Custom Derive for Zero-Copy

**Difficulty**: Insane

## The Challenge

Zero-copy serialization is one of the most powerful performance techniques available to systems programmers. Instead of parsing a byte stream into a separate in-memory data structure --- allocating, copying, validating field by field --- zero-copy deserialization interprets the raw bytes directly as the target struct. The bytes stay where they are; you just cast a pointer. This eliminates allocation overhead, reduces memory bandwidth, and can turn deserialization from an O(n) operation into an O(1) operation. Network protocols, file formats, shared memory IPC, and memory-mapped databases all benefit enormously.

But zero-copy is also one of the most dangerous techniques. If the struct has the wrong alignment, you get undefined behavior on architectures that require aligned access. If the bytes are untrusted, you can read garbage or trigger safety violations through invalid enum discriminants, dangling pointers masquerading as references, or non-UTF-8 data in what the type system claims is a `str`. Getting this right manually is tedious, error-prone, and must be repeated for every struct. This is a perfect use case for procedural macros.

Your task is to build a proc-macro crate called `zerocopy_derive` that provides `#[derive(ZeroCopy)]` for structs. The derive macro generates safe zero-copy serialization and deserialization: converting a struct to a byte slice and converting a byte slice back to a reference to the struct. The macro must enforce alignment constraints, handle endianness conversion for cross-platform compatibility, support nested structs (that also derive `ZeroCopy`), generate validation code for invariants (enum discriminants, boolean values, nonzero types), and produce clear compile-time error messages when the derive is applied to types that cannot be safely zero-copied (types with pointers, references, padding ambiguity, or non-repr-C layout). The result should be a miniature version of what crates like `zerocopy`, `bytemuck`, and `rkyv` provide, built from first principles.

## Acceptance Criteria

### Core Derive Macro

- [ ] Create a workspace with two crates: `zerocopy_derive` (proc-macro crate) and `zerocopy_types` (the traits and runtime support)
- [ ] `zerocopy_types` defines these traits:
  - `ZeroCopy: Sized` --- marker trait for types that can be safely zero-copied
  - `AsBytes: ZeroCopy` --- can be safely converted to `&[u8]`
  - `FromBytes: ZeroCopy` --- can be safely converted from `&[u8]` to `&Self`
  - `Validate` --- has a `validate(bytes: &[u8]) -> Result<(), ValidationError>` method for runtime checks
- [ ] `#[derive(ZeroCopy)]` generates implementations of `ZeroCopy`, `AsBytes`, `FromBytes`, and `Validate` for annotated structs
- [ ] The generated `as_bytes(&self) -> &[u8]` method returns a byte slice view of the struct (no copy)
- [ ] The generated `from_bytes(bytes: &[u8]) -> Result<&Self, ZeroCopyError>` method:
  - Checks that `bytes.len() == std::mem::size_of::<Self>()`
  - Checks that the pointer alignment satisfies `std::mem::align_of::<Self>()`
  - Runs validation checks on the bytes
  - Returns `&Self` by pointer cast on success
- [ ] The generated `from_bytes_unchecked(bytes: &[u8]) -> &Self` method skips validation (marked `unsafe`)
- [ ] The generated `validate(bytes: &[u8]) -> Result<(), ValidationError>` method checks all field-level invariants

### Compile-Time Safety

- [ ] Emit a compile error if `#[derive(ZeroCopy)]` is applied to a struct that is not `#[repr(C)]` or `#[repr(transparent)]`
- [ ] Emit a compile error if `#[derive(ZeroCopy)]` is applied to a struct containing:
  - Reference types (`&T`, `&mut T`)
  - Raw pointer types (`*const T`, `*mut T`)
  - `Box<T>`, `Vec<T>`, `String`, or any heap-allocated type
  - `bool` without explicit `#[zerocopy(validate)]` annotation (because `bool` has a niche and not all bit patterns are valid)
  - Types that do not themselves implement `ZeroCopy`
- [ ] Emit a compile error if `#[derive(ZeroCopy)]` is applied to an enum (enums require special handling; support only simple C-like enums with a separate `#[derive(ZeroCopyEnum)]` macro)
- [ ] Emit a compile error if `#[derive(ZeroCopy)]` is applied to a union
- [ ] Emit a compile error if the struct has generic type parameters that are not bounded by `ZeroCopy`
- [ ] All compile errors must use `proc_macro::Diagnostic` or `syn::Error` to produce errors pointing at the specific offending field or attribute, with helpful explanatory messages
- [ ] Include at least 12 compile-fail tests using `trybuild` verifying each of the above error conditions

### Endianness Support

- [ ] Support a `#[zerocopy(endian = "big")]` or `#[zerocopy(endian = "little")]` attribute on the struct to specify wire endianness
- [ ] When an endianness is specified, the generated code converts integer fields (`u16`, `u32`, `u64`, `i16`, `i32`, `i64`, `u128`, `i128`) between native and wire endianness during serialization/deserialization
- [ ] Provide wrapper types `BigEndian<T>` and `LittleEndian<T>` that store integers in the specified byte order and implement `ZeroCopy`
- [ ] The endianness conversion must be zero-cost when the native endianness matches the wire endianness (compile-time branch elimination via `cfg(target_endian)`)
- [ ] Test endianness handling with both a big-endian field layout and a little-endian field layout, verifying byte-level correctness

### Nested Struct Support

- [ ] Support structs containing fields that themselves derive `ZeroCopy`
- [ ] The generated validation recursively validates nested structs
- [ ] Handle alignment of nested structs correctly: the parent's layout must account for the child's alignment requirements
- [ ] Support arrays of `ZeroCopy` types: `[T; N]` where `T: ZeroCopy`
- [ ] Support tuples of `ZeroCopy` types up to arity 4 (implement `ZeroCopy` for `(A,)`, `(A, B)`, `(A, B, C)`, `(A, B, C, D)` where all components are `ZeroCopy` --- but only for `repr(C)` compatibility, which tuples do not have, so document this limitation and provide a workaround)

### Validation

- [ ] For `bool` fields marked with `#[zerocopy(validate)]`, generate code that checks the byte is either `0x00` or `0x01`
- [ ] For enum fields (C-like enums deriving `ZeroCopyEnum`), generate code that checks the discriminant is a valid variant
- [ ] Implement `#[derive(ZeroCopyEnum)]` for C-like enums with explicit discriminants, supporting `#[repr(u8)]`, `#[repr(u16)]`, `#[repr(u32)]`, and `#[repr(i8)]`, `#[repr(i16)]`, `#[repr(i32)]`
- [ ] For `ZeroCopyEnum`, generate `from_discriminant(value: ReprType) -> Option<Self>` and `to_discriminant(&self) -> ReprType`
- [ ] Support custom validation via `#[zerocopy(validate_with = "path::to::function")]` where the function has signature `fn(value: &T) -> Result<(), ValidationError>`
- [ ] Support range validation: `#[zerocopy(range = "1..=100")]` for integer fields
- [ ] The `ValidationError` type must carry the field name, the byte offset, and a human-readable description of the violation

### Padding and Layout

- [ ] Detect and handle padding bytes in `#[repr(C)]` structs: padding bytes must be zeroed on serialization and ignored (or optionally checked) on deserialization
- [ ] Generate a `const LAYOUT: &[FieldLayout]` associated constant that describes the offset, size, and alignment of every field (for debugging and interop)
- [ ] Provide a `#[zerocopy(packed)]` attribute that works with `#[repr(C, packed)]` structs, disabling alignment checks for fields (with a documented warning about unaligned access on some architectures)
- [ ] Compute the correct `size_of` and `align_of` at compile time and embed them as constants in the generated code, with `const_assert!` that they match `std::mem::size_of::<Self>()` and `std::mem::align_of::<Self>()`

### Mutable Access

- [ ] Provide `from_bytes_mut(bytes: &mut [u8]) -> Result<&mut Self, ZeroCopyError>` for mutable zero-copy access
- [ ] Ensure that mutation through `from_bytes_mut` cannot violate validation invariants by providing a `ValidatedMut<'a, T>` wrapper that re-validates on drop (or provides only field-by-field setter methods that maintain invariants)
- [ ] Test that modifying a struct through `from_bytes_mut` is reflected in the underlying byte slice

### Tests and Verification

- [ ] Include at least 30 tests covering:
  - Round-trip: `from_bytes(as_bytes(x)) == x` for various types
  - Alignment rejection: `from_bytes` on a misaligned buffer returns an error
  - Size rejection: `from_bytes` on a too-short or too-long buffer returns an error
  - Validation rejection: invalid bool, invalid enum discriminant, out-of-range integer
  - Endianness: byte-level verification that fields are in the specified byte order
  - Nested structs: zero-copy through multiple levels of nesting
  - Arrays: zero-copy for `[T; N]` fields
  - Padding: serialized bytes have zero padding
  - Mutable access: modify through `from_bytes_mut`, read back through `from_bytes`
  - Cross-field validation: custom validator that checks relationships between fields
- [ ] Property-based tests using `proptest` for round-trip correctness: for any valid instance of a `ZeroCopy` struct, `from_bytes(as_bytes(&value)).unwrap() == value`
- [ ] Include compile-fail tests for all compile-time error conditions (at least 12 test cases)
- [ ] Run tests under Miri (`cargo +nightly miri test`) to verify no undefined behavior in pointer casts
- [ ] Benchmark `from_bytes` against `bincode::deserialize` for a representative struct, showing the performance advantage

### Documentation

- [ ] The `zerocopy_types` crate must have a top-level doc comment explaining zero-copy serialization, when to use it, and the safety invariants
- [ ] Every generated method must have a doc comment explaining its behavior, safety requirements, and failure modes
- [ ] Include a `README.md` with usage examples, a comparison to existing crates, and a design rationale

## Starting Points

- Study the [`zerocopy` crate source](https://github.com/google/zerocopy) --- this is Google's production zero-copy library for Rust. Focus on `src/lib.rs` for the trait definitions (`FromBytes`, `AsBytes`, `Unaligned`) and `zerocopy-derive/src/lib.rs` for the derive macro implementation. Pay attention to how they handle the `FromZeroes` vs `FromBytes` distinction
- Read the [`bytemuck` crate source](https://github.com/Lokathor/bytemuck) --- a simpler alternative to `zerocopy` that uses `Pod` and `Zeroable` traits. Study `src/pod.rs` for the safety requirements and `derive/src/lib.rs` for the derive implementation
- Study the [`rkyv` crate](https://github.com/rkyv/rkyv) for a more advanced approach that supports archived (zero-copy) representations of complex types including `Vec`, `String`, and enums. Focus on `rkyv_derive/src/lib.rs`
- Read the [Rust Reference on type layout](https://doc.rust-lang.org/reference/type-layout.html), especially the sections on `repr(C)`, `repr(transparent)`, and `repr(packed)` --- these define exactly how the compiler lays out your structs in memory
- Study the [`syn` crate documentation](https://docs.rs/syn/latest/syn/) --- you will use `syn` extensively for parsing the struct definition. Focus on `DeriveInput`, `Data::Struct`, `Fields`, and `Attribute`
- Study the [`quote` crate documentation](https://docs.rs/quote/latest/quote/) --- you will use `quote!` to generate the implementation code. Understand how `#variable` interpolation works and how to handle iterators with `#(#field_impls)*`
- Read the [`proc-macro2` documentation](https://docs.rs/proc-macro2/latest/proc_macro2/) for `Span` manipulation, which is essential for good error messages
- Study [dtolnay's `proc-macro-workshop`](https://github.com/dtolnay/proc-macro-workshop) --- especially the "derive" projects (Builder, Debug, Bitfield) for patterns you will reuse
- Read the [Rustonomicon chapter on data layout](https://doc.rust-lang.org/nomicon/data.html) and [transmuting](https://doc.rust-lang.org/nomicon/transmutes.html) for understanding when pointer casts are safe
- Examine the [`static_assertions` crate](https://docs.rs/static_assertions/latest/static_assertions/) for patterns on compile-time assertions you can embed in generated code
- Look at the [`trybuild` crate](https://docs.rs/trybuild/latest/trybuild/) for compile-fail testing of proc macros

## Hints

1. Start with the trait definitions in `zerocopy_types`. The key insight is that `AsBytes` is safe for any `repr(C)` struct where all fields implement `AsBytes` (because `repr(C)` guarantees no uninitialized padding is read --- wait, it does NOT guarantee that. Padding bytes are uninitialized. You must explicitly zero them or use `repr(C, packed)` to eliminate them. This subtlety is critical).

2. For your first iteration, ignore padding. Implement the derive for a simple struct with no padding (e.g., `#[repr(C)] struct Point { x: f32, y: f32 }`). Get the `as_bytes` and `from_bytes` round-trip working. Then add padding handling.

3. The `as_bytes` implementation is straightforward: `unsafe { std::slice::from_raw_parts(self as *const Self as *const u8, std::mem::size_of::<Self>()) }`. But this is only sound if padding bytes are initialized. To guarantee this, generate a constructor or `Default` implementation that zeros the entire allocation with `MaybeUninit::zeroed()` before writing fields.

4. The `from_bytes` implementation: `let ptr = bytes.as_ptr() as *const Self; unsafe { &*ptr }`. This is only sound if: (a) the pointer is aligned, (b) the byte pattern is a valid representation of `Self`, (c) the lifetime of the returned reference does not outlive `bytes`. The alignment check is: `(ptr as usize) % std::mem::align_of::<Self>() == 0`.

5. For parsing struct attributes, use `syn::parse::<DeriveInput>()` and then check `input.attrs` for `repr`. To find `#[repr(C)]`, iterate over attrs and parse the `repr(...)` contents. Watch out: a struct can have multiple repr attributes or multiple items in one repr (e.g., `#[repr(C, packed)]`).

6. For compile-time errors, use `syn::Error::new_spanned(field, "message")` to point the error at the specific field. Collect all errors before returning so the user sees all problems at once, not just the first one. Return errors via `TokenStream::from(error.to_compile_error())`.

7. For endianness, the pattern is: when serializing, convert each integer field to the target byte order using `.to_be_bytes()` or `.to_le_bytes()`. When deserializing, convert back. But this is NOT zero-copy anymore --- you are transforming the data. The alternative is to store integers in the wire byte order always, using wrapper types like `BigEndian<u32>` that store the bytes and provide accessors.

8. The `BigEndian<u32>` wrapper should be `#[repr(transparent)]` containing `[u8; 4]`, with `get()` and `set()` methods that call `u32::from_be_bytes()` and `u32::to_be_bytes()`. This is truly zero-copy because the wrapper has alignment 1 and the bytes are never reinterpreted as a native integer in place.

9. For padding detection, compute the layout of the `repr(C)` struct manually in the proc macro. The rules are: fields are laid out in declaration order, each field is aligned to its alignment, and the struct's total size is rounded up to its alignment. Any gap between the end of one field and the start of the next is padding. Generate code that zeroes these ranges in `as_bytes`.

10. Computing layout in the proc macro is tricky because you do not know the size/alignment of field types at macro expansion time. You have two options: (a) require the user to annotate sizes, or (b) generate `const` expressions that compute offsets using `std::mem::offset_of!` (stabilized in Rust 1.77) and `std::mem::size_of`. Option (b) is better.

11. For nested struct support, the derive just needs to ensure each field type implements `ZeroCopy`. The generated validation calls `<FieldType as Validate>::validate(&bytes[offset..offset+size])` for each field. The compiler's trait system handles the recursion.

12. For the `ValidatedMut` wrapper, consider using `DerefMut` to provide transparent access but running validation in the `Drop` implementation. This is a "guard" pattern similar to `MutexGuard`. Alternatively, provide setter methods for each field that validate the new value before writing it.

13. For arrays `[T; N]`, implement `ZeroCopy` for arrays where `T: ZeroCopy` using a blanket implementation. The validation iterates over each element: `for i in 0..N { T::validate(&bytes[i*size..(i+1)*size])?; }`.

14. For the `ZeroCopyEnum` macro, parse the enum variants and discriminants. Generate a `from_discriminant` function with a match statement. The validation reads the discriminant from the byte slice (respecting the `repr` type's size and alignment) and checks it against the valid variants.

15. For benchmarking, create a representative struct with mixed field types (integers, nested structs, arrays) and compare `ZeroCopy::from_bytes()` (which should be essentially a pointer cast + validation) against `bincode::deserialize()` (which copies every field). Use `criterion` for statistically rigorous benchmarks.

16. A common bug in zero-copy implementations: forgetting that `std::mem::size_of::<T>()` includes trailing padding but `offset_of!` for the last field plus its size does not. Make sure your size checks account for trailing padding.

17. For the `trybuild` compile-fail tests, organize them by error category: `tests/ui/not_repr_c.rs`, `tests/ui/has_reference.rs`, `tests/ui/has_pointer.rs`, `tests/ui/has_vec.rs`, `tests/ui/unvalidated_bool.rs`, etc. Each should be a minimal reproduction with a clear comment explaining the expected error.

18. Use `#[proc_macro_derive(ZeroCopy, attributes(zerocopy))]` to register both the derive and the helper attribute in one declaration. This allows `#[zerocopy(...)]` on fields without the compiler complaining about unknown attributes.

19. For Miri testing, be aware that pointer alignment checks may behave differently under Miri. Allocate test buffers with explicit alignment using `#[repr(align(N))]` wrapper structs or `std::alloc::Layout` to ensure the tests exercise the intended code paths.

20. Consider implementing a `zerocopy_types::AlignedBuffer<const ALIGN: usize>` type that guarantees a byte buffer is aligned to `ALIGN`. This solves the common problem of needing to pass an aligned `&[u8]` to `from_bytes`. Use `#[repr(C, align(N))]` with a const generic. This utility type alone makes the library much more ergonomic.
