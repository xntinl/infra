# 19. Memory Layout Optimization

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-18 (ownership, traits, generics, unsafe basics)
- Understanding of stack vs heap allocation
- Familiarity with `Box`, `Vec`, `String` internals at a conceptual level
- Comfort reading compiler output and using standard library docs

## Learning Objectives

- Understand how Rust lays out structs and enums in memory by default
- Use `std::mem::size_of`, `std::mem::align_of`, and `std::mem::offset_of!` to inspect layout
- Compare `#[repr(Rust)]` (default) vs `#[repr(C)]` vs `#[repr(packed)]` vs `#[repr(transparent)]`
- Optimize struct size by reordering fields to minimize padding
- Exploit niche optimization to shrink `Option<T>` for free
- Apply `Box` to reduce enum size when variants have asymmetric payload sizes
- Design data structures with cache-line awareness and data-oriented patterns
- Evaluate when layout optimization matters vs when it is premature

## Concepts

### Why Layout Matters

Every allocation, every cache miss, and every page fault has a cost. For a struct stored once, layout is irrelevant. For a struct stored in a `Vec` of ten million elements, shaving 8 bytes per element saves 80 MB of memory and potentially halves cache misses.

Layout optimization is not about micro-benchmarks. It is about understanding what the compiler does by default, knowing when the defaults are good enough, and recognizing the scenarios where they are not.

### Inspecting Layout: size_of, align_of, offset_of

```rust
use std::mem;

struct Example {
    a: u8,
    b: u64,
    c: u8,
}

fn main() {
    println!("size:  {}", mem::size_of::<Example>());   // 24 (not 10!)
    println!("align: {}", mem::align_of::<Example>());   // 8
}
```

Why 24 instead of 10? Alignment. The `u64` field requires 8-byte alignment, so the compiler inserts padding after `a` to align `b`. Then more padding after `c` to make the struct's total size a multiple of its alignment (8).

Layout in memory (with `#[repr(C)]`):
```text
Offset  Field   Size   Padding after
0       a       1      7 bytes
8       b       8      0
16      c       1      7 bytes
Total: 24 bytes
```

### Default Layout: #[repr(Rust)]

By default, Rust makes **no guarantees** about field order or padding. The compiler is free to reorder fields to minimize the struct's size:

```rust
use std::mem;

// The compiler may reorder fields to: b, a, c
// Layout: b(8) + a(1) + c(1) + 6 padding = 16 bytes
struct Reordered {
    a: u8,
    b: u64,
    c: u8,
}

// Manually optimized field order
struct ManuallyOrdered {
    b: u64,
    a: u8,
    c: u8,
}

fn main() {
    // With repr(Rust), the compiler often produces 16 for both
    println!("Reordered size:       {}", mem::size_of::<Reordered>());
    println!("ManuallyOrdered size: {}", mem::size_of::<ManuallyOrdered>());
}
```

In practice, the Rust compiler (`rustc` with default `repr(Rust)`) reorders fields to produce optimal layout. You benefit from this without writing any extra code. The key insight: **`repr(Rust)` is usually already optimal.**

### #[repr(C)]: Predictable but Wasteful

`#[repr(C)]` uses C's layout rules: fields are laid out in declaration order with padding inserted for alignment:

```rust
use std::mem;

#[repr(C)]
struct CLayout {
    a: u8,    // offset 0, size 1, then 7 bytes padding
    b: u64,   // offset 8, size 8
    c: u8,    // offset 16, size 1, then 7 bytes padding
}

fn main() {
    println!("CLayout size: {}", mem::size_of::<CLayout>());  // 24
    // Compare with the same fields, manually reordered
}

#[repr(C)]
struct CLayoutOptimized {
    b: u64,   // offset 0, size 8
    a: u8,    // offset 8, size 1
    c: u8,    // offset 9, size 1, then 6 bytes padding
}

// CLayoutOptimized is 16 bytes -- field order matters with repr(C)
```

Use `#[repr(C)]` when:
- Interfacing with C code via FFI
- Guaranteeing a specific memory layout for serialization
- Using `memmap` or reading binary file formats

Do not use `#[repr(C)]` "just in case" -- it prevents the compiler from optimizing layout.

### #[repr(packed)] and #[repr(align)]

`#[repr(packed)]` removes all padding. Fields may be unaligned, which is undefined behavior on some platforms if you take references to them:

```rust
#[repr(C, packed)]
struct Packed {
    a: u8,
    b: u64,
    c: u8,
}
// Size: 10 bytes (no padding)
// WARNING: &self.b is an unaligned reference. Reading it requires
// copying to a local variable first.

fn read_b(p: &Packed) -> u64 {
    // Safe: Rust 2021+ automatically copies unaligned fields
    // when you access them through a reference to a packed struct
    p.b
}
```

`#[repr(align(N))]` forces a minimum alignment:

```rust
#[repr(align(64))]
struct CacheAligned {
    data: [u8; 32],
}
// Size: 64 bytes (padded to fill one cache line)
// Alignment: 64 bytes
```

Use `align(64)` to prevent false sharing in concurrent data structures, where two threads access different fields that happen to share a cache line.

### #[repr(transparent)]

`#[repr(transparent)]` guarantees a struct has the same layout as its single non-zero-sized field:

```rust
#[repr(transparent)]
struct UserId(u64);

#[repr(transparent)]
struct Meters(f64);
```

This is essential for newtype wrappers used in FFI: it guarantees `UserId` and `u64` are ABI-compatible.

### Padding and Field Ordering Rules

The general alignment rules:

1. Each field is aligned to its own alignment requirement
2. Padding is inserted before a field to meet its alignment
3. The struct's alignment is the maximum alignment of any field
4. The struct's size is rounded up to a multiple of its alignment

```rust
use std::mem;

// Worst case: alternating large and small fields
#[repr(C)]
struct Worst {
    a: u8,     // 1 byte + 7 padding
    b: u64,    // 8 bytes
    c: u8,     // 1 byte + 3 padding
    d: u32,    // 4 bytes
    e: u8,     // 1 byte + 7 padding
    f: u64,    // 8 bytes
}
// Total: 40 bytes for 22 bytes of actual data

// Best case: fields sorted by alignment (largest first)
#[repr(C)]
struct Best {
    b: u64,    // 8 bytes
    f: u64,    // 8 bytes
    d: u32,    // 4 bytes
    a: u8,     // 1 byte
    c: u8,     // 1 byte
    e: u8,     // 1 byte + 1 padding
}
// Total: 24 bytes for 22 bytes of actual data

fn main() {
    println!("Worst: {}", mem::size_of::<Worst>());  // 40
    println!("Best:  {}", mem::size_of::<Best>());    // 24
}
```

**Rule of thumb for `repr(C)`**: sort fields by alignment, largest first. With default `repr(Rust)`, the compiler does this for you.

### Enum Layout and Tag Size

Enums store a discriminant (tag) plus the largest variant's payload:

```rust
use std::mem;

enum Small {
    A,
    B(u8),
    C(u16),
}

enum Large {
    A,
    B([u8; 256]),
    C(u8),
}

fn main() {
    println!("Small: {}", mem::size_of::<Small>());  // 4 (tag + u16 + padding)
    println!("Large: {}", mem::size_of::<Large>());  // 257+ (tag + 256 bytes)
}
```

The size of every instance of `Large` is dictated by the biggest variant (`B`), even when most instances are `A` or `C`. This is where `Box` helps.

### Enum Size Optimization with Box

When one variant is much larger than the rest, box its payload:

```rust
use std::mem;

// Before: every instance is 264+ bytes
enum Ast {
    Literal(i64),
    BinaryOp {
        op: char,
        left: Box<Ast>,
        right: Box<Ast>,
    },
    Block {
        statements: Vec<Ast>,        // 24 bytes (Vec is pointer+len+cap)
        local_variables: [u8; 256],   // 256 bytes -- dominates the enum size
    },
}

// After: Box the large variant
enum AstOptimized {
    Literal(i64),
    BinaryOp {
        op: char,
        left: Box<AstOptimized>,
        right: Box<AstOptimized>,
    },
    Block(Box<BlockData>),  // Now just a pointer (8 bytes)
}

struct BlockData {
    statements: Vec<AstOptimized>,
    local_variables: [u8; 256],
}

fn main() {
    println!("Ast size:          {}", mem::size_of::<Ast>());
    println!("AstOptimized size: {}", mem::size_of::<AstOptimized>());
    // AstOptimized is ~24 bytes vs Ast's ~290+ bytes
}
```

**Trade-off**: you add one heap allocation per `Block` node. For ASTs where `Block` is rare, this is a massive win. For data where every element is a `Block`, the indirection adds cache misses.

### Niche Optimization: Option<T> for Free

Rust's `Option<T>` is the same size as `T` when `T` has a *niche* -- an invalid bit pattern that represents `None`:

```rust
use std::mem;

fn main() {
    // References are never null, so None uses the null pointer
    println!("&u64:         {}", mem::size_of::<&u64>());            // 8
    println!("Option<&u64>: {}", mem::size_of::<Option<&u64>>());    // 8 (same!)

    // Box is never null
    println!("Box<u64>:         {}", mem::size_of::<Box<u64>>());           // 8
    println!("Option<Box<u64>>: {}", mem::size_of::<Option<Box<u64>>>());   // 8

    // NonZeroU64 cannot be zero, so None uses the zero value
    use std::num::NonZeroU64;
    println!("NonZeroU64:         {}", mem::size_of::<NonZeroU64>());          // 8
    println!("Option<NonZeroU64>: {}", mem::size_of::<Option<NonZeroU64>>()); // 8

    // Regular u64 has no niche: all bit patterns are valid
    println!("u64:         {}", mem::size_of::<u64>());            // 8
    println!("Option<u64>: {}", mem::size_of::<Option<u64>>());    // 16 (tag needed)

    // bool uses 0 and 1, leaving 254 niches
    println!("bool:         {}", mem::size_of::<bool>());           // 1
    println!("Option<bool>: {}", mem::size_of::<Option<bool>>());   // 1
}
```

Types with niches: references, `Box`, `NonZero*`, `bool`, enums with fewer than 256 variants, `char` (not all u32 values are valid Unicode scalars).

**Design implication**: if you need an optional ID, use `Option<NonZeroU64>` instead of `Option<u64>` to save 8 bytes per instance.

### Nested Niche Optimization

Niche optimization composes:

```rust
use std::mem;
use std::num::NonZeroU32;

fn main() {
    // Option<Option<NonZeroU32>> is still 4 bytes!
    // None(outer) = 0x00000000
    // Some(None)  = impossible to distinguish... wait:
    // Actually: Option<NonZeroU32> has niche (0), so
    // Option<Option<NonZeroU32>> uses a second niche value
    println!("NonZeroU32:                     {}", mem::size_of::<NonZeroU32>());                    // 4
    println!("Option<NonZeroU32>:             {}", mem::size_of::<Option<NonZeroU32>>());            // 4
    println!("Option<Option<NonZeroU32>>:     {}", mem::size_of::<Option<Option<NonZeroU32>>>());    // 4

    // Enum with unused discriminant values also has niches
    enum Direction { North, South, East, West }
    println!("Direction:         {}", mem::size_of::<Direction>());         // 1
    println!("Option<Direction>: {}", mem::size_of::<Option<Direction>>()); // 1
}
```

### Cache Lines and Data-Oriented Design

A modern CPU cache line is 64 bytes. When the CPU reads one byte from memory, it fetches the entire 64-byte line. Designing data structures to work *with* the cache is called data-oriented design.

**Array of Structs (AoS) vs Struct of Arrays (SoA):**

```rust
// Array of Structs: each entity's fields are contiguous
struct EntityAoS {
    position: [f32; 3],  // 12 bytes
    velocity: [f32; 3],  // 12 bytes
    health: f32,          // 4 bytes
    name: String,         // 24 bytes (pointer + len + cap)
}

// If you iterate and only read `health`, you load 52 bytes per entity
// but only use 4. That is 92% wasted bandwidth.

// Struct of Arrays: each field is a separate contiguous array
struct EntitiesSoA {
    positions:  Vec<[f32; 3]>,
    velocities: Vec<[f32; 3]>,
    healths:    Vec<f32>,
    names:      Vec<String>,
}

// Now iterating over `healths` loads only f32 values. 16 health values
// fit in one 64-byte cache line. 16x better cache utilization for this
// access pattern.
```

**When to use SoA:**
- Hot loops that access a single field across many entities (game physics, ECS)
- SIMD-friendly operations on contiguous numeric arrays
- Very large collections (millions of elements)

**When AoS is fine:**
- Small collections (hundreds of elements)
- Access patterns that touch all fields of each entity
- Code clarity matters more than performance

### Measuring Layout in Practice

```rust
use std::mem;

macro_rules! print_layout {
    ($t:ty) => {
        println!(
            "{:<40} size={:<4} align={}",
            stringify!($t),
            mem::size_of::<$t>(),
            mem::align_of::<$t>(),
        );
    };
}

fn main() {
    print_layout!(u8);
    print_layout!(u16);
    print_layout!(u32);
    print_layout!(u64);
    print_layout!(u128);
    print_layout!(bool);
    print_layout!(char);
    print_layout!(&str);
    print_layout!(String);
    print_layout!(Vec<u8>);
    print_layout!(Box<[u8]>);
    print_layout!(Option<&u8>);
    print_layout!(Option<Box<u8>>);
    print_layout!(Option<u8>);
    print_layout!(Result<u8, u8>);
    print_layout!(Result<(), Box<dyn std::error::Error>>);
}
```

### offset_of! Macro (Stable since Rust 1.77)

```rust
use std::mem;

#[repr(C)]
struct Header {
    magic: u32,
    version: u16,
    flags: u8,
    reserved: u8,
    payload_len: u64,
}

fn main() {
    println!("magic offset:       {}", mem::offset_of!(Header, magic));        // 0
    println!("version offset:     {}", mem::offset_of!(Header, version));      // 4
    println!("flags offset:       {}", mem::offset_of!(Header, flags));        // 6
    println!("reserved offset:    {}", mem::offset_of!(Header, reserved));     // 7
    println!("payload_len offset: {}", mem::offset_of!(Header, payload_len));  // 8
    println!("total size:         {}", mem::size_of::<Header>());              // 16
}
```

## Exercises

### Exercise 1: Audit and Optimize a Struct

Given the following struct used in a game engine, audit its memory layout and optimize it.

**Cargo.toml:**
```toml
[package]
name = "layout-exercises"
version = "0.1.0"
edition = "2021"
```

**Original struct:**
```rust
struct GameEntity {
    is_active: bool,         // 1 byte
    health: f64,             // 8 bytes
    entity_type: u8,         // 1 byte
    position_x: f64,         // 8 bytes
    shield: u8,              // 1 byte
    position_y: f64,         // 8 bytes
    team_id: u16,            // 2 bytes
    velocity_x: f32,         // 4 bytes
    is_visible: bool,        // 1 byte
    velocity_y: f32,         // 4 bytes
}
```

**Requirements:**
1. Print the size and alignment of the original struct (with `repr(C)` to see worst-case)
2. Create an optimized version by reordering fields (still `repr(C)`)
3. Create a version using default `repr(Rust)` and compare
4. Use `NonZeroU16` for `team_id` to enable niche optimization on `Option<GameEntity>`
5. Write assertions that verify the optimized version is smaller
6. Compare `Option<OriginalEntity>` vs `Option<OptimizedEntity>` sizes

**Hints:**
- Sort fields by alignment: f64 first, then f32, then u16, then u8/bool
- Total data: 8+8+8+4+4+2+1+1+1+1 = 38 bytes
- Optimal `repr(C)` alignment packing: 40 bytes (38 rounded up to 8-byte boundary)

<details>
<summary>Solution</summary>

```rust
use std::mem;
use std::num::NonZeroU16;

// Original: worst-case field ordering with repr(C)
#[repr(C)]
struct GameEntityOriginal {
    is_active: bool,         // offset 0,  1 byte + 7 padding
    health: f64,             // offset 8,  8 bytes
    entity_type: u8,         // offset 16, 1 byte + 7 padding
    position_x: f64,         // offset 24, 8 bytes
    shield: u8,              // offset 32, 1 byte + 7 padding
    position_y: f64,         // offset 40, 8 bytes
    team_id: u16,            // offset 48, 2 bytes + 2 padding
    velocity_x: f32,         // offset 52, 4 bytes
    is_visible: bool,        // offset 56, 1 byte + 3 padding
    velocity_y: f32,         // offset 60, 4 bytes
}
// Total: 64 bytes

// Optimized: fields sorted by alignment, largest first
#[repr(C)]
struct GameEntityOptimized {
    // 8-byte aligned fields first
    health: f64,             // offset 0,  8 bytes
    position_x: f64,         // offset 8,  8 bytes
    position_y: f64,         // offset 16, 8 bytes
    // 4-byte aligned fields
    velocity_x: f32,         // offset 24, 4 bytes
    velocity_y: f32,         // offset 28, 4 bytes
    // 2-byte aligned fields
    team_id: u16,            // offset 32, 2 bytes
    // 1-byte aligned fields
    entity_type: u8,         // offset 34, 1 byte
    shield: u8,              // offset 35, 1 byte
    is_active: bool,         // offset 36, 1 byte
    is_visible: bool,        // offset 37, 1 byte + 2 padding
}
// Total: 40 bytes

// Default repr(Rust): compiler reorders for us
struct GameEntityDefault {
    is_active: bool,
    health: f64,
    entity_type: u8,
    position_x: f64,
    shield: u8,
    position_y: f64,
    team_id: u16,
    velocity_x: f32,
    is_visible: bool,
    velocity_y: f32,
}

// With NonZeroU16 for niche optimization
struct GameEntityNiche {
    health: f64,
    position_x: f64,
    position_y: f64,
    velocity_x: f32,
    velocity_y: f32,
    team_id: NonZeroU16,    // niche: 0 is not a valid team
    entity_type: u8,
    shield: u8,
    is_active: bool,
    is_visible: bool,
}

macro_rules! print_layout {
    ($name:literal, $t:ty) => {
        println!("{:<30} size={:<4} align={}", $name, mem::size_of::<$t>(), mem::align_of::<$t>());
    };
}

fn main() {
    println!("=== Struct Sizes ===\n");
    print_layout!("Original (repr C)",   GameEntityOriginal);
    print_layout!("Optimized (repr C)",  GameEntityOptimized);
    print_layout!("Default (repr Rust)", GameEntityDefault);
    print_layout!("Niche (repr Rust)",   GameEntityNiche);

    println!("\n=== Option Sizes ===\n");
    print_layout!("Option<Original>",  Option<GameEntityOriginal>);
    print_layout!("Option<Optimized>", Option<GameEntityOptimized>);
    print_layout!("Option<Default>",   Option<GameEntityDefault>);
    print_layout!("Option<Niche>",     Option<GameEntityNiche>);

    println!("\n=== Savings ===\n");
    let original = mem::size_of::<GameEntityOriginal>();
    let optimized = mem::size_of::<GameEntityOptimized>();
    let default = mem::size_of::<GameEntityDefault>();
    println!("repr(C) savings:   {} bytes ({:.0}% reduction)",
        original - optimized,
        (1.0 - optimized as f64 / original as f64) * 100.0);
    println!("repr(Rust) vs repr(C) worst: {} bytes saved", original - default);

    println!("\n=== Per-Million Entities ===\n");
    let n = 1_000_000usize;
    println!("Original:  {} MB", original * n / 1_048_576);
    println!("Optimized: {} MB", optimized * n / 1_048_576);
    println!("Default:   {} MB", default * n / 1_048_576);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn optimized_is_smaller_than_original() {
        assert!(
            mem::size_of::<GameEntityOptimized>() < mem::size_of::<GameEntityOriginal>(),
            "optimized ({}) should be smaller than original ({})",
            mem::size_of::<GameEntityOptimized>(),
            mem::size_of::<GameEntityOriginal>(),
        );
    }

    #[test]
    fn optimized_repr_c_is_40_bytes() {
        assert_eq!(mem::size_of::<GameEntityOptimized>(), 40);
    }

    #[test]
    fn default_repr_is_at_most_optimized_repr_c() {
        // repr(Rust) should be at least as good as manually optimized repr(C)
        assert!(
            mem::size_of::<GameEntityDefault>() <= mem::size_of::<GameEntityOptimized>(),
            "default ({}) should be <= optimized ({})",
            mem::size_of::<GameEntityDefault>(),
            mem::size_of::<GameEntityOptimized>(),
        );
    }

    #[test]
    fn niche_option_is_same_size() {
        // With NonZeroU16, Option should not add a discriminant
        // (depends on compiler layout decisions for the whole struct)
        let base = mem::size_of::<GameEntityNiche>();
        let option = mem::size_of::<Option<GameEntityNiche>>();
        // At minimum, Option should be no more than base + alignment
        assert!(option <= base + mem::align_of::<GameEntityNiche>(),
            "Option<Niche> ({option}) is too large compared to base ({base})");
    }

    #[test]
    fn all_alignments_are_8() {
        assert_eq!(mem::align_of::<GameEntityOriginal>(), 8);
        assert_eq!(mem::align_of::<GameEntityOptimized>(), 8);
        assert_eq!(mem::align_of::<GameEntityDefault>(), 8);
    }

    #[test]
    fn million_entities_memory() {
        let n = 1_000_000;
        let original_mb = mem::size_of::<GameEntityOriginal>() * n / 1_048_576;
        let optimized_mb = mem::size_of::<GameEntityOptimized>() * n / 1_048_576;
        // Optimized saves at least 20MB per million entities
        assert!(original_mb - optimized_mb >= 20,
            "should save significant memory: original={original_mb}MB, optimized={optimized_mb}MB");
    }
}
```

**Key insight:** With `repr(C)`, field order is your responsibility. Sorting by alignment (largest first) minimizes padding. With default `repr(Rust)`, the compiler handles this. Use `repr(C)` only when you need a specific layout (FFI, binary formats).
</details>

### Exercise 2: Enum Size Reduction with Boxing

Build an AST for a simple configuration language. Optimize enum size by boxing large variants.

**Node types:**
- `Integer(i64)` -- 8 bytes
- `Float(f64)` -- 8 bytes
- `Text(String)` -- 24 bytes
- `Boolean(bool)` -- 1 byte
- `List(Vec<ConfigValue>)` -- 24 bytes
- `Map(Vec<(String, ConfigValue)>)` -- 24 bytes
- `Include { path: String, optional: bool, fallback: Option<Box<ConfigValue>> }` -- ~40 bytes
- `Template { name: String, params: Vec<String>, body: Vec<ConfigValue> }` -- ~72 bytes

**Requirements:**
1. Create a `ConfigValueUnoptimized` enum with all variants inline
2. Create a `ConfigValueOptimized` enum where large variants are boxed
3. Verify the size reduction with assertions
4. Write a function that builds a sample config tree using the optimized version
5. Verify both versions produce the same logical behavior

**Hints:**
- The unoptimized enum's size is dominated by the `Template` variant
- Boxing variants larger than 24 bytes is a good heuristic
- After boxing, the enum should be around 32 bytes (tag + Vec/String)

<details>
<summary>Solution</summary>

```rust
use std::mem;

// --- Unoptimized: every instance pays for the largest variant ---

#[derive(Debug, Clone, PartialEq)]
enum ConfigValueUnoptimized {
    Integer(i64),
    Float(f64),
    Text(String),
    Boolean(bool),
    List(Vec<ConfigValueUnoptimized>),
    Map(Vec<(String, ConfigValueUnoptimized)>),
    Include {
        path: String,
        optional: bool,
        fallback: Option<Box<ConfigValueUnoptimized>>,
    },
    Template {
        name: String,
        params: Vec<String>,
        body: Vec<ConfigValueUnoptimized>,
    },
}

// --- Optimized: large variants are boxed ---

#[derive(Debug, Clone, PartialEq)]
enum ConfigValue {
    Integer(i64),
    Float(f64),
    Text(String),
    Boolean(bool),
    List(Vec<ConfigValue>),
    Map(Vec<(String, ConfigValue)>),
    Include(Box<IncludeData>),
    Template(Box<TemplateData>),
}

#[derive(Debug, Clone, PartialEq)]
struct IncludeData {
    path: String,
    optional: bool,
    fallback: Option<Box<ConfigValue>>,
}

#[derive(Debug, Clone, PartialEq)]
struct TemplateData {
    name: String,
    params: Vec<String>,
    body: Vec<ConfigValue>,
}

impl ConfigValue {
    fn as_integer(&self) -> Option<i64> {
        match self {
            ConfigValue::Integer(n) => Some(*n),
            _ => None,
        }
    }

    fn as_text(&self) -> Option<&str> {
        match self {
            ConfigValue::Text(s) => Some(s),
            _ => None,
        }
    }

    fn as_list(&self) -> Option<&[ConfigValue]> {
        match self {
            ConfigValue::List(v) => Some(v),
            _ => None,
        }
    }
}

fn build_sample_config() -> ConfigValue {
    ConfigValue::Map(vec![
        ("name".into(), ConfigValue::Text("my-app".into())),
        ("port".into(), ConfigValue::Integer(8080)),
        ("debug".into(), ConfigValue::Boolean(false)),
        ("database".into(), ConfigValue::Map(vec![
            ("host".into(), ConfigValue::Text("localhost".into())),
            ("port".into(), ConfigValue::Integer(5432)),
        ])),
        ("features".into(), ConfigValue::List(vec![
            ConfigValue::Text("auth".into()),
            ConfigValue::Text("metrics".into()),
        ])),
        ("base".into(), ConfigValue::Include(Box::new(IncludeData {
            path: "./base.toml".into(),
            optional: true,
            fallback: Some(Box::new(ConfigValue::Map(vec![]))),
        }))),
        ("routes".into(), ConfigValue::Template(Box::new(TemplateData {
            name: "standard_routes".into(),
            params: vec!["prefix".into(), "version".into()],
            body: vec![
                ConfigValue::Text("/api/v1/health".into()),
                ConfigValue::Text("/api/v1/ready".into()),
            ],
        }))),
    ])
}

fn count_nodes(val: &ConfigValue) -> usize {
    match val {
        ConfigValue::Integer(_) | ConfigValue::Float(_) |
        ConfigValue::Text(_) | ConfigValue::Boolean(_) => 1,
        ConfigValue::List(items) => 1 + items.iter().map(count_nodes).sum::<usize>(),
        ConfigValue::Map(entries) => 1 + entries.iter().map(|(_, v)| count_nodes(v)).sum::<usize>(),
        ConfigValue::Include(data) => {
            1 + data.fallback.as_ref().map_or(0, |f| count_nodes(f))
        }
        ConfigValue::Template(data) => {
            1 + data.body.iter().map(count_nodes).sum::<usize>()
        }
    }
}

fn main() {
    println!("=== Enum Sizes ===\n");
    println!("ConfigValueUnoptimized: {} bytes", mem::size_of::<ConfigValueUnoptimized>());
    println!("ConfigValue (optimized): {} bytes", mem::size_of::<ConfigValue>());
    println!("IncludeData: {} bytes", mem::size_of::<IncludeData>());
    println!("TemplateData: {} bytes", mem::size_of::<TemplateData>());

    let savings = mem::size_of::<ConfigValueUnoptimized>() as f64
        / mem::size_of::<ConfigValue>() as f64;
    println!("\nSize ratio: {:.1}x smaller", savings);

    println!("\n=== Option Sizes ===\n");
    println!("Option<Unoptimized>: {} bytes", mem::size_of::<Option<ConfigValueUnoptimized>>());
    println!("Option<Optimized>:   {} bytes", mem::size_of::<Option<ConfigValue>>());

    println!("\n=== Sample Config ===\n");
    let config = build_sample_config();
    println!("Nodes: {}", count_nodes(&config));
    println!("{:#?}", config);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn optimized_is_smaller() {
        let unopt = mem::size_of::<ConfigValueUnoptimized>();
        let opt = mem::size_of::<ConfigValue>();
        assert!(opt < unopt,
            "optimized ({opt}) should be smaller than unoptimized ({unopt})");
    }

    #[test]
    fn optimized_is_at_most_32_bytes() {
        // Vec is 24 bytes, tag is up to 8, so 32 is the expected max
        assert!(mem::size_of::<ConfigValue>() <= 32,
            "optimized enum should be at most 32 bytes, got {}",
            mem::size_of::<ConfigValue>());
    }

    #[test]
    fn sample_config_structure() {
        let config = build_sample_config();
        let nodes = count_nodes(&config);
        assert!(nodes > 10, "sample config should have many nodes, got {nodes}");
    }

    #[test]
    fn accessors_work() {
        assert_eq!(ConfigValue::Integer(42).as_integer(), Some(42));
        assert_eq!(ConfigValue::Text("hi".into()).as_text(), Some("hi"));
        assert_eq!(ConfigValue::Boolean(true).as_integer(), None);
    }

    #[test]
    fn include_data_accessible() {
        let include = ConfigValue::Include(Box::new(IncludeData {
            path: "test.toml".into(),
            optional: false,
            fallback: None,
        }));
        match &include {
            ConfigValue::Include(data) => {
                assert_eq!(data.path, "test.toml");
                assert!(!data.optional);
                assert!(data.fallback.is_none());
            }
            _ => panic!("expected Include variant"),
        }
    }

    #[test]
    fn template_data_accessible() {
        let tmpl = ConfigValue::Template(Box::new(TemplateData {
            name: "test".into(),
            params: vec!["a".into()],
            body: vec![ConfigValue::Integer(1)],
        }));
        match &tmpl {
            ConfigValue::Template(data) => {
                assert_eq!(data.name, "test");
                assert_eq!(data.params.len(), 1);
                assert_eq!(data.body.len(), 1);
            }
            _ => panic!("expected Template variant"),
        }
    }

    #[test]
    fn per_million_memory_comparison() {
        let n = 1_000_000;
        let unopt_mb = mem::size_of::<ConfigValueUnoptimized>() * n / 1_048_576;
        let opt_mb = mem::size_of::<ConfigValue>() * n / 1_048_576;
        println!("Unoptimized: {unopt_mb} MB, Optimized: {opt_mb} MB");
        assert!(unopt_mb > opt_mb);
    }
}
```

**When to box enum variants:**
- One variant is 2x or more larger than the median variant size
- The enum is stored in large collections (Vec, HashMap)
- The large variant is used infrequently relative to smaller variants

**When not to box:**
- All variants are similar in size
- The enum is only used in a few places (not stored in collections)
- The large variant is the most common one (boxing just adds indirection)
</details>

### Exercise 3: Data-Oriented Design -- AoS vs SoA

Build a particle simulation where 100,000 particles have position, velocity, color, and lifetime. Compare AoS and SoA layouts for a physics update loop.

**Requirements:**
1. Implement an AoS layout (`Vec<Particle>`)
2. Implement an SoA layout (`ParticleSystem` with separate `Vec`s)
3. Both must implement `update(dt: f32)` that applies velocity to position and decreases lifetime
4. Benchmark both with `std::time::Instant`
5. Implement `count_alive()` that counts particles with `lifetime > 0`
6. Compare the performance of `count_alive()` between the two layouts

**Hints:**
- SoA shines when you access one field across all particles
- `count_alive()` only reads `lifetime` -- SoA loads 4 bytes per particle, AoS loads 52+
- Run benchmarks in release mode (`cargo run --release`)

<details>
<summary>Solution</summary>

```rust
use std::mem;
use std::time::Instant;

// === Array of Structs ===

#[derive(Debug, Clone)]
struct Particle {
    pos_x: f32,
    pos_y: f32,
    pos_z: f32,
    vel_x: f32,
    vel_y: f32,
    vel_z: f32,
    color_r: u8,
    color_g: u8,
    color_b: u8,
    color_a: u8,
    lifetime: f32,
    size: f32,
}

struct ParticlesAoS {
    particles: Vec<Particle>,
}

impl ParticlesAoS {
    fn new(count: usize) -> Self {
        let particles = (0..count)
            .map(|i| {
                let fi = i as f32;
                Particle {
                    pos_x: fi * 0.01,
                    pos_y: fi * 0.02,
                    pos_z: fi * 0.03,
                    vel_x: 1.0,
                    vel_y: 0.5,
                    vel_z: -0.1,
                    color_r: (i % 256) as u8,
                    color_g: ((i * 7) % 256) as u8,
                    color_b: ((i * 13) % 256) as u8,
                    color_a: 255,
                    lifetime: 10.0 - (i % 20) as f32 * 0.5,
                    size: 1.0 + (i % 5) as f32 * 0.2,
                }
            })
            .collect();
        ParticlesAoS { particles }
    }

    fn update(&mut self, dt: f32) {
        for p in &mut self.particles {
            p.pos_x += p.vel_x * dt;
            p.pos_y += p.vel_y * dt;
            p.pos_z += p.vel_z * dt;
            p.lifetime -= dt;
        }
    }

    fn count_alive(&self) -> usize {
        self.particles.iter().filter(|p| p.lifetime > 0.0).count()
    }

    fn memory_bytes(&self) -> usize {
        self.particles.len() * mem::size_of::<Particle>()
    }
}

// === Struct of Arrays ===

struct ParticlesSoA {
    pos_x: Vec<f32>,
    pos_y: Vec<f32>,
    pos_z: Vec<f32>,
    vel_x: Vec<f32>,
    vel_y: Vec<f32>,
    vel_z: Vec<f32>,
    color_r: Vec<u8>,
    color_g: Vec<u8>,
    color_b: Vec<u8>,
    color_a: Vec<u8>,
    lifetime: Vec<f32>,
    size: Vec<f32>,
}

impl ParticlesSoA {
    fn new(count: usize) -> Self {
        let mut sys = ParticlesSoA {
            pos_x: Vec::with_capacity(count),
            pos_y: Vec::with_capacity(count),
            pos_z: Vec::with_capacity(count),
            vel_x: Vec::with_capacity(count),
            vel_y: Vec::with_capacity(count),
            vel_z: Vec::with_capacity(count),
            color_r: Vec::with_capacity(count),
            color_g: Vec::with_capacity(count),
            color_b: Vec::with_capacity(count),
            color_a: Vec::with_capacity(count),
            lifetime: Vec::with_capacity(count),
            size: Vec::with_capacity(count),
        };
        for i in 0..count {
            let fi = i as f32;
            sys.pos_x.push(fi * 0.01);
            sys.pos_y.push(fi * 0.02);
            sys.pos_z.push(fi * 0.03);
            sys.vel_x.push(1.0);
            sys.vel_y.push(0.5);
            sys.vel_z.push(-0.1);
            sys.color_r.push((i % 256) as u8);
            sys.color_g.push(((i * 7) % 256) as u8);
            sys.color_b.push(((i * 13) % 256) as u8);
            sys.color_a.push(255);
            sys.lifetime.push(10.0 - (i % 20) as f32 * 0.5);
            sys.size.push(1.0 + (i % 5) as f32 * 0.2);
        }
        sys
    }

    fn len(&self) -> usize {
        self.pos_x.len()
    }

    fn update(&mut self, dt: f32) {
        let n = self.len();
        for i in 0..n {
            self.pos_x[i] += self.vel_x[i] * dt;
            self.pos_y[i] += self.vel_y[i] * dt;
            self.pos_z[i] += self.vel_z[i] * dt;
            self.lifetime[i] -= dt;
        }
    }

    fn count_alive(&self) -> usize {
        self.lifetime.iter().filter(|&&lt| lt > 0.0).count()
    }

    fn memory_bytes(&self) -> usize {
        // 8 f32 vecs + 4 u8 vecs
        self.len() * (8 * mem::size_of::<f32>() + 4 * mem::size_of::<u8>())
    }
}

fn bench<F: FnMut()>(name: &str, iterations: usize, mut f: F) -> std::time::Duration {
    let start = Instant::now();
    for _ in 0..iterations {
        f();
    }
    let elapsed = start.elapsed();
    let per_iter = elapsed / iterations as u32;
    println!("  {name:<30} total={elapsed:.2?}  per_iter={per_iter:.2?}");
    elapsed
}

fn main() {
    let count = 100_000;
    let iterations = 1000;
    let dt = 0.016; // ~60fps

    println!("=== Memory Layout ===\n");
    println!("Particle struct size: {} bytes", mem::size_of::<Particle>());
    println!("Particle alignment:   {} bytes", mem::align_of::<Particle>());

    let aos = ParticlesAoS::new(count);
    let soa = ParticlesSoA::new(count);
    println!("\nAoS memory: {:.2} MB", aos.memory_bytes() as f64 / 1_048_576.0);
    println!("SoA memory: {:.2} MB", soa.memory_bytes() as f64 / 1_048_576.0);

    println!("\n=== Physics Update ({count} particles, {iterations} iterations) ===\n");

    let mut aos = ParticlesAoS::new(count);
    let aos_time = bench("AoS update", iterations, || aos.update(dt));

    let mut soa = ParticlesSoA::new(count);
    let soa_time = bench("SoA update", iterations, || soa.update(dt));

    let ratio = aos_time.as_secs_f64() / soa_time.as_secs_f64();
    println!("\n  Update ratio: SoA is {ratio:.2}x vs AoS");

    println!("\n=== count_alive ({count} particles, {iterations} iterations) ===\n");

    let aos = ParticlesAoS::new(count);
    let soa = ParticlesSoA::new(count);

    let mut aos_alive = 0;
    let aos_count_time = bench("AoS count_alive", iterations, || {
        aos_alive = aos.count_alive();
    });

    let mut soa_alive = 0;
    let soa_count_time = bench("SoA count_alive", iterations, || {
        soa_alive = soa.count_alive();
    });

    assert_eq!(aos_alive, soa_alive, "both layouts should agree on alive count");
    let count_ratio = aos_count_time.as_secs_f64() / soa_count_time.as_secs_f64();
    println!("\n  count_alive ratio: SoA is {count_ratio:.2}x vs AoS");
    println!("  alive particles: {aos_alive}");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn particle_size_audit() {
        let size = mem::size_of::<Particle>();
        println!("Particle size: {size} bytes");
        // 6 f32 (24) + 4 u8 (4) + 2 f32 (8) = 36 data bytes
        // With alignment, likely 36 or 40
        assert!(size <= 48, "Particle should be compact, got {size}");
    }

    #[test]
    fn aos_and_soa_agree_on_count_alive() {
        let count = 1000;
        let aos = ParticlesAoS::new(count);
        let soa = ParticlesSoA::new(count);
        assert_eq!(aos.count_alive(), soa.count_alive());
    }

    #[test]
    fn update_modifies_position() {
        let mut aos = ParticlesAoS::new(10);
        let old_x = aos.particles[0].pos_x;
        aos.update(1.0);
        assert!(aos.particles[0].pos_x > old_x, "position should increase");
    }

    #[test]
    fn update_decreases_lifetime() {
        let mut soa = ParticlesSoA::new(10);
        let old_lt = soa.lifetime[0];
        soa.update(0.1);
        assert!(soa.lifetime[0] < old_lt, "lifetime should decrease");
    }

    #[test]
    fn soa_memory_is_less_or_equal_to_aos() {
        let count = 10_000;
        let aos = ParticlesAoS::new(count);
        let soa = ParticlesSoA::new(count);
        // SoA eliminates padding between fields
        assert!(soa.memory_bytes() <= aos.memory_bytes(),
            "SoA ({}) should use <= memory than AoS ({})",
            soa.memory_bytes(), aos.memory_bytes());
    }

    #[test]
    fn cache_line_analysis() {
        let particle_size = mem::size_of::<Particle>();
        let particles_per_cache_line = 64 / particle_size; // integer division
        let f32s_per_cache_line = 64 / mem::size_of::<f32>();

        println!("Particles per 64-byte cache line (AoS): {particles_per_cache_line}");
        println!("f32 lifetimes per 64-byte cache line (SoA): {f32s_per_cache_line}");

        // SoA fits 16 lifetimes per cache line; AoS fits 1-2 full particles
        assert!(f32s_per_cache_line > particles_per_cache_line * 4,
            "SoA should fit many more relevant values per cache line");
    }
}
```

**Why SoA wins for `count_alive()`:** The function only reads `lifetime`. In AoS layout, each `Particle` is ~40 bytes, so one 64-byte cache line holds ~1.5 particles -- you load color, position, and velocity data you never use. In SoA layout, `lifetime` values are contiguous `f32`s, so one cache line holds 16 lifetimes. The CPU fetches 16x more useful data per cache access.

**Why the update loop difference is smaller:** The update loop touches 7 fields (3 position + 3 velocity + 1 lifetime). AoS loads most of the struct anyway, so the cache waste is less severe. SoA still wins because the compiler can auto-vectorize contiguous float operations with SIMD, but the margin is narrower.
</details>

## Trade-Off Analysis

### When to Optimize Layout

| Scenario | Optimize? | Why |
|----------|-----------|-----|
| Struct stored once (config) | No | Saving 16 bytes is meaningless |
| Vec of 10,000 elements | Maybe | Profile first; 160KB savings may not matter |
| Vec of 10,000,000 elements | Yes | 160MB savings reduces cache misses dramatically |
| FFI struct | Yes | You must match the C layout exactly |
| Network protocol struct | Yes | Every byte matters over the wire |
| Hot loop touching one field | Yes (SoA) | Cache utilization dominates performance |
| Code read by many developers | Careful | Readability cost of non-obvious field ordering |

### repr(C) vs repr(Rust) Decision

| Factor | repr(C) | repr(Rust) (default) |
|--------|---------|---------------------|
| Field order | Deterministic (declaration order) | Compiler chooses optimal order |
| Padding | You must minimize manually | Compiler minimizes automatically |
| FFI compatible | Yes | No |
| Niche optimization | Limited | Full |
| Binary serialization | Safe (stable layout) | Unsafe (layout may change between compiler versions) |
| Performance | As good as your field ordering | Usually optimal |

### Boxing Large Enum Variants

| Factor | Inline (no Box) | Boxed |
|--------|-----------------|-------|
| Size per instance | Largest variant dictates | Pointer size (8 bytes) |
| Heap allocations | Zero | One per boxed instance |
| Cache locality | Data is inline | Pointer chase on access |
| Best when | All variants similar size | One variant much larger than rest |
| Pattern matching | Direct field access | Must dereference Box |

## Common Mistakes

1. **Optimizing layout before profiling.** If your program spends 99% of its time in I/O, shaving 8 bytes off a struct changes nothing. Profile first with `perf`, `flamegraph`, or `cargo bench`.

2. **Using `#[repr(C)]` without FFI need.** You disable the compiler's layout optimizer for no benefit. Default `repr(Rust)` is almost always better for pure Rust code.

3. **Forgetting that `#[repr(packed)]` creates unaligned references.** Taking `&self.field` on a packed struct may cause undefined behavior or crashes on architectures that require alignment.

4. **Assuming SoA is always faster.** If your access pattern touches all fields of each element, AoS has better locality because all fields are adjacent. SoA only wins when you access a subset of fields across many elements.

5. **Not checking `Option<T>` sizes.** Many developers assume `Option<T>` is always larger than `T`. For references, `Box`, `NonZero*`, and small enums, it is the same size.

## Verification

```bash
# Run all tests and see layout information
cargo test -- --nocapture

# Run the benchmarks in release mode (important for meaningful numbers)
cargo run --release

# Check with clippy
cargo clippy -- -D warnings

# Verify specific layout sizes
cargo test particle_size_audit -- --nocapture
cargo test optimized_is_smaller -- --nocapture
```

## What You Learned

- **Default `repr(Rust)`** reorders fields automatically -- manual field ordering is only needed for `repr(C)`
- **Padding** is inserted to satisfy alignment requirements; sorting fields by alignment (largest first) minimizes it in `repr(C)`
- **Niche optimization** makes `Option<T>` zero-cost for references, `Box`, `NonZero*`, `bool`, and small enums
- **Boxing large enum variants** reduces the size of every instance when one variant dominates
- **SoA layout** dramatically improves cache utilization when hot loops access a subset of fields across many elements
- **`std::mem::size_of`**, **`align_of`**, and **`offset_of!`** are your tools for auditing layout
- **Profile before optimizing** -- layout changes only matter for large collections or hot loops

## What's Next

Exercise 20 explores advanced closures and Fn traits -- understanding the Fn/FnMut/FnOnce hierarchy, capture semantics, and how to store and return closures effectively.

## Resources

- [The Rust Reference: Type Layout](https://doc.rust-lang.org/reference/type-layout.html)
- [std::mem module](https://doc.rust-lang.org/std/mem/index.html)
- [Niche optimization in Rust](https://rust-lang.github.io/unsafe-code-guidelines/layout/enums.html)
- [Data-Oriented Design (book)](https://www.dataorienteddesign.com/dodbook/)
- [Cache-Oblivious Algorithms](https://en.wikipedia.org/wiki/Cache-oblivious_algorithm)
- [Rustonomicon: repr](https://doc.rust-lang.org/nomicon/other-reprs.html)
- [offset_of! stabilization](https://doc.rust-lang.org/std/mem/macro.offset_of.html)
