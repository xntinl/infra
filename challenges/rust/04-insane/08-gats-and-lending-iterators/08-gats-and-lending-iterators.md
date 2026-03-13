# 8. GATs and Lending Iterators

**Difficulty**: Insane

## The Challenge

Rust's `Iterator` trait has a fundamental limitation: the `Item` type cannot borrow from the iterator itself. This means you cannot write an iterator that yields `&'a str` slices into a buffer the iterator owns and mutates between calls. Generic Associated Types (GATs), stabilized in Rust 1.65, unlock this pattern.

Build a lending iterator library that provides:

1. **A `LendingIterator` trait** where the `Item` type is parameterized by the lifetime of each borrow:

```rust
trait LendingIterator {
    type Item<'a> where Self: 'a;
    fn next(&mut self) -> Option<Self::Item<'_>>;
}
```

2. **A `WindowsMut` iterator** that yields overlapping mutable windows into a slice — something `std::slice::Windows` cannot do because it would require multiple mutable references. Your lending iterator yields one window at a time, and the caller must drop it before advancing.

3. **A streaming CSV parser** that reads chunks from a `BufRead`, parses fields in-place, and yields `&[&str]` rows referencing an internal buffer — zero allocation per row.

4. **A type-family trait** using GATs that abstracts over container kinds:

```rust
trait Collection {
    type Elem;
    type Iter<'a>: Iterator<Item = &'a Self::Elem> where Self: 'a;
    fn iter(&self) -> Self::Iter<'_>;
    fn push(&mut self, elem: Self::Elem);
}
```

Implement this for `Vec<T>`, `BTreeSet<T>`, and `VecDeque<T>`.

This exercise forces you to reason about lifetime parameterization at the type level — the same machinery that underlies async fn in traits and zero-cost abstractions over borrowing patterns.

## Acceptance Criteria

- [ ] `LendingIterator` trait compiles with GAT syntax and the `where Self: 'a` bound
- [ ] `WindowsMut<'_, T>` implements `LendingIterator` and yields `&mut [T]` windows of configurable size
- [ ] Overlapping mutable windows work correctly: `[1,2,3,4,5]` with window size 3 yields `[1,2,3]`, `[2,3,4]`, `[3,4,5]`
- [ ] The compiler rejects code that holds two windows simultaneously (borrow checker enforces single active borrow)
- [ ] Streaming CSV parser implements `LendingIterator` with `Item<'a> = &'a [&'a str]` (or equivalent)
- [ ] CSV parser allocates no heap memory per row (reuses internal buffer); verify with a custom allocator or `dhat`
- [ ] `Collection` trait implemented for `Vec<T>`, `BTreeSet<T>`, `VecDeque<T>` with correct `Iter` GAT
- [ ] Generic function written over `Collection` that works across all three container types
- [ ] Adapter combinators on `LendingIterator`: at minimum `map`, `filter`, `take`, `for_each`
- [ ] Demonstrate that `LendingIterator` cannot implement `Iterator` (explain why in a doc comment)
- [ ] All code compiles on stable Rust (1.65+)

## Starting Points

- RFC 1598: Generic Associated Types — read the motivation section to understand why this required a language change and could not be emulated with existing features.
- Niko Matsakis's blog series on GATs: "The Push for GATs Stabilization" (2021-2022) traces the design decisions.
- The `lending-iterator` crate by Daniel Henry-Mantilla (danielhenrymantilla) — study the source, especially the lifetime threading in adapter types.
- `std::iter::Iterator` source in `core/src/iter/traits/iterator.rs` — compare the associated type design and understand why `type Item` (no lifetime parameter) prevents lending.
- The `streaming-iterator` crate (pre-GAT workaround) — study how it solves a subset of the problem without GATs and where it falls short.

## Hints

1. The `where Self: 'a` bound on the GAT is not optional. Without it, the compiler cannot prove that a reference to `Self` outlives the `'a` in `Item<'a>`. This was one of the key stabilization blockers — understanding why this bound is needed requires thinking about what happens when `Self` contains references with shorter lifetimes.

2. `WindowsMut` must store a `&mut [T]` and an index. Each call to `next()` reborrows from the stored mutable reference. The trick: you need to split the mutable borrow so the yielded window borrows from `self`, not from the original slice. Study how `std::slice::IterMut` handles this with raw pointers.

3. The CSV parser's buffer strategy: read a line into an owned `String` buffer, split it in-place to find field boundaries, store start/end indices, then yield references into the buffer. The `Item` lifetime ties the yielded references to the borrow of `&mut self`, ensuring the buffer is not modified while references are live.

4. Adapter types like `Map<L, F>` for `LendingIterator` require higher-ranked trait bounds: `F: for<'a> FnMut(L::Item<'a>) -> R`. Getting these bounds right is the core difficulty. Some adapters (like `collect`) are fundamentally impossible for lending iterators — reason about why.

5. You cannot blanket-impl `Iterator for T where T: LendingIterator` because `Iterator::Item` has no lifetime parameter. The reverse is possible. Think about what a `into_iter` adapter would require (the item must be owned or `'static`).

## Going Further

- Implement `lending_iter.by_ref()` — this requires a GAT on the `LendingIterator` impl for `&mut L`.
- Build a `LendingIterator`-based database cursor that yields rows borrowing from an internal page buffer, simulating how real database drivers avoid per-row allocation.
- Implement async lending iteration: `async fn next(&mut self) -> Option<Self::Item<'_>>`. Explore the interaction between GATs, async, and `Pin`.
- Compare your lending iterator with the `gen` blocks RFC (RFC 3513) — could generators replace lending iterators?
- Benchmark your zero-alloc CSV parser against the `csv` crate on a large file. Measure throughput and allocation counts.

## Resources

- **RFC**: [RFC 1598 — Generic Associated Types](https://rust-lang.github.io/rfcs/1598-generic_associated_types.html)
- **Blog**: Niko Matsakis — "The Push for GATs Stabilization" — [smallcultfollowing.com](https://smallcultfollowing.com/babysteps/)
- **Blog**: Sabrina Jewson — "The Better Alternative to Lifetime GATs" — explores HRTB-based alternatives
- **Crate source**: `lending-iterator` — [github.com/danielhenrymantilla/lending-iterator.rs](https://github.com/danielhenrymantilla/lending-iterator.rs)
- **Crate source**: `streaming-iterator` — [github.com/sfackler/streaming-iterator](https://github.com/sfackler/streaming-iterator)
- **Source**: `core::iter::traits::iterator` — Rust stdlib source
- **Talk**: Niko Matsakis — "Rust 2024 and Beyond" (various conference talks on GATs and async)
- **Book**: Jon Gjengset — *Rust for Rustaceans*, Chapter 1 (Types and Traits), GAT discussion
- **Stabilization report**: [Tracking issue #44265](https://github.com/rust-lang/rust/issues/44265) — read the stabilization comment for the final design rationale
- **Blog**: Yoshua Wuyts — "Async Iteration" — discusses how GATs enable async streams
