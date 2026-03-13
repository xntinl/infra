# 5. Pin, Unpin, and Self-Referential Structs

**Difficulty**: Insane

## The Challenge

Build a self-referential struct from scratch — safely, unsafely, and with the
`pin-project` crate — and implement a non-trivial `Future` by hand that depends on
pinning guarantees to be sound.

`Pin` exists because of a single problem: async/await compiles to state machines
that contain references to their own fields. If you move such a struct in memory,
those internal pointers dangle. `Pin` is the API contract that says "this value will
not move." It is the most misunderstood feature in Rust, and most explanations fail
because they show the API without showing the problem it solves.

You will create the problem, feel the pain, build the solution, and then implement a
real future that requires it.

## Acceptance Criteria

### Part 1: Create the Problem
- [ ] Build a self-referential struct `SelfRef` containing a `String` and a `*const str`
      that points into that `String`'s buffer
- [ ] Demonstrate the unsoundness: create a `SelfRef`, move it (via `mem::swap` or
      assignment), show that the internal pointer now dangles
- [ ] Verify the dangling pointer by printing the pointed-to data before and after
      the move — it will be garbage or a segfault after the move

### Part 2: Pin as the Solution
- [ ] Wrap `SelfRef` in `Pin<Box<SelfRef>>` so it cannot be moved
- [ ] Mark `SelfRef` as `!Unpin` (via `PhantomPinned`)
- [ ] Implement a safe `new()` that constructs the self-reference after pinning
- [ ] Demonstrate that `mem::swap` on the pinned value is now a compile error
- [ ] Implement accessor methods on `Pin<&SelfRef>` and `Pin<&mut SelfRef>` using
      `unsafe` pin projection where needed

### Part 3: Pin Projection
- [ ] Implement structural pinning manually: given a pinned `Pin<&mut Outer>`,
      project to `Pin<&mut Inner>` for a field that is structurally pinned, and
      `&mut Other` for a field that is not
- [ ] Enumerate the four rules for safe structural pinning (from the `pin` module docs)
      and verify your projection satisfies all four
- [ ] Reimplement the same projections using `pin-project-lite`'s `pin_project!` macro
- [ ] Write a test that verifies the projected references are valid after the parent
      is pinned

### Part 4: Implement Future by Hand
- [ ] Build a `Delay` future that resolves after N polls (not time-based — just a
      counter, to isolate the pinning concern from I/O)
- [ ] Build a `Chain` future combinator: given two futures A and B, poll A to
      completion, store its output in `self`, then poll B with access to A's output.
      This requires the output of A to be stored alongside the still-polling B —
      a self-referential situation.
- [ ] The `Chain` future must be `!Unpin` and must work correctly when pinned
- [ ] Implement `Future` for `Chain` manually using `unsafe` pin projections
- [ ] Demonstrate `Chain` working in a real executor (tokio or your own from exercise 1)

### Part 5: Understand the Ecosystem
- [ ] Compare your manual `Chain` implementation with what `async { let a = fut_a.await; fut_b(a).await }` compiles to (use `cargo expand` or `rustc --emit=mir`)
- [ ] Show that the compiler-generated future is `!Unpin` and explain why
- [ ] Demonstrate that `Box::pin(async { ... })` is the escape hatch when you need
      to store an `!Unpin` future in a struct without infecting the parent with `Pin`

## Starting Points

- **pin module docs** (`std::pin`): Read the entire module documentation in the
  standard library. It is long and dense but it is the authoritative source.
  Pay special attention to the "Structural pinning" section and the `Drop` guarantee.
- **pin-project source** (`taiki-e/pin-project`): Study `pin-project-internal/src/pin_project/derive.rs`
  for how the derive macro generates pin projections. The `__PinProjectionFields`
  helper struct and the `UnsafeUnpin` implementation reveal the exact safety
  conditions.
- **pin-project-lite source** (`taiki-e/pin-project-lite`): Study `src/lib.rs` — the
  entire crate is a single `macro_rules!` macro. Simpler than the proc-macro version
  and instructive for understanding what the generated code looks like.
- **tokio::pin! macro**: Study `tokio/tokio/src/macros/pin.rs`. This macro pins a
  value to the stack using `unsafe`. Understand why it is sound: the pinned value
  is shadowed by a `Pin<&mut T>` that borrows it, preventing the original binding
  from being used (and thus preventing moves).
- **Rust RFC 2349** (`Pin` RFC): The design document for the entire `Pin` API.
  Read sections on "Motivation" and "Rationale and alternatives" for the design
  space that was explored.

## Hints

1. The self-referential struct problem in three sentences: struct has field `data: String`
   and field `ptr: *const str` where `ptr` points into `data`'s heap buffer. Moving
   the struct moves `data` (and its `String` metadata) but the heap buffer stays put —
   so `ptr` is actually still valid in this specific case. The real danger is with
   stack-pinned values or when `String` reallocates. The async case is worse: the
   compiler generates a struct where one field is a reference to another field, not
   a raw pointer, and moving it is instant UB.

2. `!Unpin` means "this type has made pinning guarantees and must not be moved once
   pinned." `Unpin` means "pinning is irrelevant; this type is safe to move even
   when behind `Pin`." Most types are `Unpin` (it is an auto trait). The `!Unpin`
   types are: futures generated by `async {}`, types containing `PhantomPinned`,
   and types that opt out manually.

3. The four rules for structural pinning (from the `Pin` docs): (a) the struct must
   not implement `Unpin` if the field is structurally pinned, (b) the struct must not
   offer any API that moves the field after pinning, (c) the struct's `Drop` impl
   must not move the field, (d) the struct must uphold the `Pin` drop guarantee
   (the field is not deallocated or overwritten without `drop` being called).

4. For the `Chain` combinator, the state machine has two phases: `PollingA { a: A }`
   and `PollingB { a_output: A::Output, b: B }`. The transition from phase 1 to
   phase 2 requires moving `A::Output` into the struct while `B` is still there.
   Since `B` might be `!Unpin`, you must be careful about pin projection during
   the transition. This is the exact problem the compiler solves for `async {}` blocks.

5. `cargo expand` (via `cargo-expand`) on an `async fn` will show you the anonymous
   type the compiler generates. It is an `enum` with one variant per `await` point.
   The variants contain the local variables that are live across that point. When a
   local variable is a reference to another local, the variant is self-referential.

## Going Further

- Study the `ouroboros` crate (`somber/ouroboros`): it uses proc macros to generate
  safe self-referential structs by carefully managing lifetimes and preventing access
  to the "tail" (borrowed) field except through closures. Reimplement its core
  pattern without the macro.
- Study the `self_cell` crate: a simpler alternative to ouroboros. It uses a two-phase
  construction pattern where the dependent value is created in a closure that receives
  a reference to the owner.
- Implement an intrusive linked list where nodes are pinned in place and contain
  `Pin`-projected pointers to other nodes. This is how tokio's internal waiters work
  (`tokio/src/util/linked_list.rs`).
- Build a `Stream` (async iterator) combinator that chains two streams, requiring
  pinning of the underlying streams. Contrast with the `futures::stream::Chain`
  implementation.
- Read the generator RFC and understand how generators (the foundation of async/await)
  produce `!Unpin` types. Experiment with `#![feature(generators)]` on nightly.

## Resources

- [std::pin module documentation](https://doc.rust-lang.org/std/pin/index.html) —
  The authoritative reference on pinning
- [RFC 2349: Pin](https://rust-lang.github.io/rfcs/2349-pin.html) — Original design
  document
- [pin-project source](https://github.com/taiki-e/pin-project) — Safe pin projection
  derive macro
- [pin-project-lite source](https://github.com/taiki-e/pin-project-lite) —
  Declarative macro alternative
- [ouroboros source](https://github.com/somber-dream/ouroboros) — Self-referential
  struct generation
- [self_cell source](https://github.com/Voultapher/self_cell) — Simpler
  self-referential struct pattern
- [withoutboats: "Pin"](https://without.boats/blog/pin/) — The original blog post
  series on the design of Pin by its author
- [fasterthanlime: "Pin and Suffering"](https://fasterthanli.me/articles/pin-and-suffering) —
  Practical walkthrough with diagrams of why Pin exists
- [Jon Gjengset: "The What and How of Futures and async/await in Rust"](https://www.youtube.com/watch?v=9_3krAQtD2k) —
  Covers the connection between Pin and async
- [Rust Nomicon: PhantomData](https://doc.rust-lang.org/nomicon/phantom-data.html) —
  For understanding `PhantomPinned`'s role
- [boats: "Async Destructors"](https://without.boats/blog/async-destructors/) —
  On the interaction between `Pin`, `Drop`, and async
