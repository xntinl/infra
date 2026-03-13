# Rust Challenges & Exercises

> 145 practical Rust exercises organized in 4 difficulty levels.
> From first `cargo new` to building async runtimes, database engines, and competitive programming.
> Each exercise includes learning objectives, compilable code, verification commands, and references.

**Difficulty Levels**:
- **Basic** (15) — Full step-by-step guidance, complete code, every term explained
- **Intermediate** (35) — Guided steps with TODO gaps, pattern vs anti-pattern comparisons, competitive programming fundamentals
- **Advanced** (50) — Problem + hints + one solution, trade-off analysis, production patterns, competitive programming algorithms
- **Insane** (45) — Problem statement + acceptance criteria only, no code provided

**Requirements**:
- Rust installed via [rustup](https://rustup.rs)
- `cargo` and `rustc` available
- A terminal and text editor

**Convention**: Each exercise uses `cargo new` for a fresh project. Clean up with `rm -rf` when done.

---

### 01 — Basico

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Hello Rust and Cargo](01-basico/01-hello-rust-cargo/01-hello-rust-cargo.md) | Basic |
| 02 | [Variables, Mutability, Shadowing](01-basico/02-variables-mutability-shadowing/02-variables-mutability-shadowing.md) | Basic |
| 03 | [Scalar Types](01-basico/03-scalar-types/03-scalar-types.md) | Basic |
| 04 | [Compound Types](01-basico/04-compound-types/04-compound-types.md) | Basic |
| 05 | [Functions and Expressions](01-basico/05-functions-and-expressions/05-functions-and-expressions.md) | Basic |
| 06 | [Control Flow](01-basico/06-control-flow/06-control-flow.md) | Basic |
| 07 | [Ownership](01-basico/07-ownership/07-ownership.md) | Basic |
| 08 | [References and Borrowing](01-basico/08-references-and-borrowing/08-references-and-borrowing.md) | Basic |
| 09 | [The Slice Type](01-basico/09-the-slice-type/09-the-slice-type.md) | Basic |
| 10 | [Structs and Methods](01-basico/10-structs-and-methods/10-structs-and-methods.md) | Basic |
| 11 | [Enums and Pattern Matching](01-basico/11-enums-and-pattern-matching/11-enums-and-pattern-matching.md) | Basic |
| 12 | [Option and Result](01-basico/12-option-and-result/12-option-and-result.md) | Basic |
| 13 | [Vectors](01-basico/13-vectors/13-vectors.md) | Basic |
| 14 | [Strings](01-basico/14-strings/14-strings.md) | Basic |
| 15 | [HashMaps and Collections](01-basico/15-hashmaps-and-collections/15-hashmaps-and-collections.md) | Basic |

---

### 02 — Intermedio

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Traits](02-intermedio/01-traits/01-traits.md) | Intermediate |
| 02 | [Generics](02-intermedio/02-generics/02-generics.md) | Intermediate |
| 03 | [Lifetimes](02-intermedio/03-lifetimes/03-lifetimes.md) | Intermediate |
| 04 | [Closures](02-intermedio/04-closures/04-closures.md) | Intermediate |
| 05 | [Iterators](02-intermedio/05-iterators/05-iterators.md) | Intermediate |
| 06 | [Smart Pointers](02-intermedio/06-smart-pointers/06-smart-pointers.md) | Intermediate |
| 07 | [Error Handling Patterns](02-intermedio/07-error-handling-patterns/07-error-handling-patterns.md) | Intermediate |
| 08 | [Testing](02-intermedio/08-testing/08-testing.md) | Intermediate |
| 09 | [Modules and Visibility](02-intermedio/09-modules-and-visibility/09-modules-and-visibility.md) | Intermediate |
| 10 | [Cargo and Dependencies](02-intermedio/10-cargo-and-dependencies/10-cargo-and-dependencies.md) | Intermediate |
| 11 | [Type Conversions](02-intermedio/11-type-conversions/11-type-conversions.md) | Intermediate |
| 12 | [Operator Overloading](02-intermedio/12-operator-overloading/12-operator-overloading.md) | Intermediate |
| 13 | [Newtype Pattern](02-intermedio/13-newtype-pattern/13-newtype-pattern.md) | Intermediate |
| 14 | [Builder Pattern](02-intermedio/14-builder-pattern/14-builder-pattern.md) | Intermediate |
| 15 | [State Machine Pattern](02-intermedio/15-state-machine-pattern/15-state-machine-pattern.md) | Intermediate |
| 16 | [Recursive Data Structures](02-intermedio/16-recursive-data-structures/16-recursive-data-structures.md) | Intermediate |
| 17 | [Iterator Adapters and Custom Iterators](02-intermedio/17-iterator-adapters-custom-iterators/17-iterator-adapters-custom-iterators.md) | Intermediate |
| 18 | [File I/O and Filesystem](02-intermedio/18-file-io-and-filesystem/18-file-io-and-filesystem.md) | Intermediate |
| 19 | [Regular Expressions](02-intermedio/19-regular-expressions/19-regular-expressions.md) | Intermediate |
| 20 | [CP: Two Pointers and Sliding Window](02-intermedio/20-cp-two-pointers-sliding-window/20-cp-two-pointers-sliding-window.md) | Intermediate |
| 21 | [CP: Binary Search Patterns](02-intermedio/21-cp-binary-search-patterns/21-cp-binary-search-patterns.md) | Intermediate |
| 22 | [CP: Sorting and Comparators](02-intermedio/22-cp-sorting-and-comparators/22-cp-sorting-and-comparators.md) | Intermediate |
| 23 | [CP: Stack and Queue Problems](02-intermedio/23-cp-stack-and-queue-problems/23-cp-stack-and-queue-problems.md) | Intermediate |
| 24 | [CP: Greedy Algorithms](02-intermedio/24-cp-greedy-algorithms/24-cp-greedy-algorithms.md) | Intermediate |
| 25 | [CP: Basic Graph BFS/DFS](02-intermedio/25-cp-basic-graph-bfs-dfs/25-cp-basic-graph-bfs-dfs.md) | Intermediate |
| 26 | [Trait Objects and Dynamic Dispatch](02-intermedio/26-trait-objects-dynamic-dispatch/26-trait-objects-dynamic-dispatch.md) | Intermediate |
| 27 | [Interior Mutability](02-intermedio/27-interior-mutability/27-interior-mutability.md) | Intermediate |
| 28 | [Phantom Types and Marker Traits](02-intermedio/28-phantom-types-marker-traits/28-phantom-types-marker-traits.md) | Intermediate |
| 29 | [Advanced Pattern Matching](02-intermedio/29-advanced-pattern-matching/29-advanced-pattern-matching.md) | Intermediate |
| 30 | [CP: HashMaps and Counting](02-intermedio/30-cp-hashmaps-and-counting/30-cp-hashmaps-and-counting.md) | Intermediate |
| 31 | [CP: Recursion and Backtracking](02-intermedio/31-cp-recursion-and-backtracking/31-cp-recursion-and-backtracking.md) | Intermediate |
| 32 | [CP: Prefix Sums and Difference Arrays](02-intermedio/32-cp-prefix-sums-and-difference-arrays/32-cp-prefix-sums-and-difference-arrays.md) | Intermediate |
| 33 | [CP: Simulation and Implementation](02-intermedio/33-cp-simulation-and-implementation/33-cp-simulation-and-implementation.md) | Intermediate |
| 34 | [CP: Basic Number Theory](02-intermedio/34-cp-basic-number-theory/34-cp-basic-number-theory.md) | Intermediate |
| 35 | [CP: Basic Dynamic Programming](02-intermedio/35-cp-basic-dynamic-programming/35-cp-basic-dynamic-programming.md) | Intermediate |

---

### 03 — Avanzado

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Threads and Spawn](03-avanzado/01-threads-and-spawn/01-threads-and-spawn.md) | Advanced |
| 02 | [Message Passing](03-avanzado/02-message-passing/02-message-passing.md) | Advanced |
| 03 | [Shared State Concurrency](03-avanzado/03-shared-state-concurrency/03-shared-state-concurrency.md) | Advanced |
| 04 | [Async/Await Fundamentals](03-avanzado/04-async-await-fundamentals/04-async-await-fundamentals.md) | Advanced |
| 05 | [Tokio Runtime](03-avanzado/05-tokio-runtime/05-tokio-runtime.md) | Advanced |
| 06 | [Async Streams and Patterns](03-avanzado/06-async-streams-and-patterns/06-async-streams-and-patterns.md) | Advanced |
| 07 | [Declarative Macros](03-avanzado/07-declarative-macros/07-declarative-macros.md) | Advanced |
| 08 | [Procedural Macros](03-avanzado/08-procedural-macros/08-procedural-macros.md) | Advanced |
| 09 | [Unsafe Rust](03-avanzado/09-unsafe-rust/09-unsafe-rust.md) | Advanced |
| 10 | [Advanced Traits](03-avanzado/10-advanced-traits/10-advanced-traits.md) | Advanced |
| 11 | [Advanced Lifetimes](03-avanzado/11-advanced-lifetimes/11-advanced-lifetimes.md) | Advanced |
| 12 | [FFI and C Interop](03-avanzado/12-ffi-and-c-interop/12-ffi-and-c-interop.md) | Advanced |
| 13 | [Serde and Serialization](03-avanzado/13-serde-and-serialization/13-serde-and-serialization.md) | Advanced |
| 14 | [Performance Optimization](03-avanzado/14-performance-optimization/14-performance-optimization.md) | Advanced |
| 15 | [WebAssembly with Rust](03-avanzado/15-wasm-with-rust/15-wasm-with-rust.md) | Advanced |
| 16 | [Advanced Error Architecture](03-avanzado/16-advanced-error-architecture/16-advanced-error-architecture.md) | Advanced |
| 17 | [Compile-Time Guarantees](03-avanzado/17-compile-time-guarantees/17-compile-time-guarantees.md) | Advanced |
| 18 | [Property-Based Testing](03-avanzado/18-property-based-testing/18-property-based-testing.md) | Advanced |
| 19 | [Memory Layout Optimization](03-avanzado/19-memory-layout-optimization/19-memory-layout-optimization.md) | Advanced |
| 20 | [Advanced Closures and Fn Traits](03-avanzado/20-advanced-closures-fn-traits/20-advanced-closures-fn-traits.md) | Advanced |
| 21 | [Concurrency Patterns](03-avanzado/21-concurrency-patterns/21-concurrency-patterns.md) | Advanced |
| 22 | [Networking with Tokio](03-avanzado/22-networking-with-tokio/22-networking-with-tokio.md) | Advanced |
| 23 | [Database Patterns](03-avanzado/23-database-patterns-sqlx/23-database-patterns-sqlx.md) | Advanced |
| 24 | [Advanced Macro Patterns](03-avanzado/24-advanced-macro-patterns/24-advanced-macro-patterns.md) | Advanced |
| 25 | [Zero-Copy Deserialization](03-avanzado/25-zero-copy-deserialization/25-zero-copy-deserialization.md) | Advanced |
| 26 | [Tracing and Structured Logging](03-avanzado/26-tracing-and-structured-logging/26-tracing-and-structured-logging.md) | Advanced |
| 27 | [CLI Applications with Clap](03-avanzado/27-cli-applications-with-clap/27-cli-applications-with-clap.md) | Advanced |
| 28 | [Async Cancellation Safety](03-avanzado/28-async-cancellation-safety/28-async-cancellation-safety.md) | Advanced |
| 29 | [Cross-Compilation and Targets](03-avanzado/29-cross-compilation-and-targets/29-cross-compilation-and-targets.md) | Advanced |
| 30 | [Rust Design Patterns](03-avanzado/30-rust-design-patterns/30-rust-design-patterns.md) | Advanced |
| 31 | [CP: Dynamic Programming Advanced](03-avanzado/31-cp-dynamic-programming-advanced/31-cp-dynamic-programming-advanced.md) | Advanced |
| 32 | [CP: Graph Shortest Paths](03-avanzado/32-cp-graph-shortest-paths/32-cp-graph-shortest-paths.md) | Advanced |
| 33 | [CP: Union-Find / Disjoint Sets](03-avanzado/33-cp-union-find-disjoint-sets/33-cp-union-find-disjoint-sets.md) | Advanced |
| 34 | [CP: Segment Trees](03-avanzado/34-cp-segment-trees/34-cp-segment-trees.md) | Advanced |
| 35 | [CP: String Algorithms](03-avanzado/35-cp-string-algorithms/35-cp-string-algorithms.md) | Advanced |
| 36 | [CP: Bit Manipulation](03-avanzado/36-cp-bit-manipulation/36-cp-bit-manipulation.md) | Advanced |
| 37 | [CP: Topological Sort](03-avanzado/37-cp-topological-sort/37-cp-topological-sort.md) | Advanced |
| 38 | [CP: Minimum Spanning Tree](03-avanzado/38-cp-minimum-spanning-tree/38-cp-minimum-spanning-tree.md) | Advanced |
| 39 | [gRPC with Tonic](03-avanzado/39-grpc-with-tonic/39-grpc-with-tonic.md) | Advanced |
| 40 | [Tower Middleware and Service Trait](03-avanzado/40-tower-middleware-service-trait/40-tower-middleware-service-trait.md) | Advanced |
| 41 | [Axum Web Framework](03-avanzado/41-axum-web-framework/41-axum-web-framework.md) | Advanced |
| 42 | [Async Channels and Actor Model](03-avanzado/42-async-channels-actor-model/42-async-channels-actor-model.md) | Advanced |
| 43 | [Rate Limiting and Backpressure](03-avanzado/43-rate-limiting-and-backpressure/43-rate-limiting-and-backpressure.md) | Advanced |
| 44 | [Graceful Shutdown Patterns](03-avanzado/44-graceful-shutdown-patterns/44-graceful-shutdown-patterns.md) | Advanced |
| 45 | [CP: Fenwick Tree / BIT](03-avanzado/45-cp-fenwick-tree/45-cp-fenwick-tree.md) | Advanced |
| 46 | [CP: Trie Data Structure](03-avanzado/46-cp-trie-data-structure/46-cp-trie-data-structure.md) | Advanced |
| 47 | [CP: Monotonic Stack and Queue](03-avanzado/47-cp-monotonic-stack-queue/47-cp-monotonic-stack-queue.md) | Advanced |
| 48 | [Build a Key-Value Store](03-avanzado/48-build-key-value-store/48-build-key-value-store.md) | Advanced |
| 49 | [Build a Load Balancer](03-avanzado/49-build-load-balancer/49-build-load-balancer.md) | Advanced |
| 50 | [Build a Task Scheduler](03-avanzado/50-build-task-scheduler/50-build-task-scheduler.md) | Advanced |

---

### 04 — Insane

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Build an Async Runtime](04-insane/01-build-async-runtime/01-build-async-runtime.md) | Insane |
| 02 | [Lock-Free Data Structures](04-insane/02-lock-free-data-structures/02-lock-free-data-structures.md) | Insane |
| 03 | [Custom Allocators](04-insane/03-custom-allocators/03-custom-allocators.md) | Insane |
| 04 | [Type-Level Programming](04-insane/04-type-level-programming/04-type-level-programming.md) | Insane |
| 05 | [Pin, Unpin, Self-Referential](04-insane/05-pin-unpin-self-referential/05-pin-unpin-self-referential.md) | Insane |
| 06 | [Const Generics and Computation](04-insane/06-const-generics-and-computation/06-const-generics-and-computation.md) | Insane |
| 07 | [Advanced Procedural Macros](04-insane/07-advanced-procedural-macros/07-advanced-procedural-macros.md) | Insane |
| 08 | [GATs and Lending Iterators](04-insane/08-gats-and-lending-iterators/08-gats-and-lending-iterators.md) | Insane |
| 09 | [Memory Model and Atomics](04-insane/09-memory-model-and-atomics/09-memory-model-and-atomics.md) | Insane |
| 10 | [SIMD and Vectorization](04-insane/10-simd-and-vectorization/10-simd-and-vectorization.md) | Insane |
| 11 | [no_std and Embedded](04-insane/11-no-std-and-embedded/11-no-std-and-embedded.md) | Insane |
| 12 | [Unsafe Abstractions](04-insane/12-unsafe-abstractions/12-unsafe-abstractions.md) | Insane |
| 13 | [Custom Async I/O Driver](04-insane/13-custom-async-io-driver/13-custom-async-io-driver.md) | Insane |
| 14 | [Formal Verification](04-insane/14-formal-verification-kani/14-formal-verification-kani.md) | Insane |
| 15 | [JIT Compilation with Cranelift](04-insane/15-jit-compilation-cranelift/15-jit-compilation-cranelift.md) | Insane |
| 16 | [Building a Language VM](04-insane/16-building-a-language-vm/16-building-a-language-vm.md) | Insane |
| 17 | [Variance and Subtyping Deep Dive](04-insane/17-variance-and-subtyping-deep-dive/17-variance-and-subtyping-deep-dive.md) | Insane |
| 18 | [Distributed Systems Primitives](04-insane/18-distributed-systems-primitives/18-distributed-systems-primitives.md) | Insane |
| 19 | [Custom Derive for Zero-Copy](04-insane/19-custom-derive-zero-copy/19-custom-derive-zero-copy.md) | Insane |
| 20 | [Writing a Compiler Frontend](04-insane/20-writing-a-compiler-frontend/20-writing-a-compiler-frontend.md) | Insane |
| 21 | [Rust GPU Compute](04-insane/21-rust-gpu-compute/21-rust-gpu-compute.md) | Insane |
| 22 | [Custom Smart Pointers and GC](04-insane/22-custom-smart-pointers-gc/22-custom-smart-pointers-gc.md) | Insane |
| 23 | [WASM Component Model](04-insane/23-wasm-component-model/23-wasm-component-model.md) | Insane |
| 24 | [Effect System Simulation](04-insane/24-effect-system-simulation/24-effect-system-simulation.md) | Insane |
| 25 | [Dynamic Plugin System](04-insane/25-dynamic-plugin-system/25-dynamic-plugin-system.md) | Insane |
| 26 | [Build a Database Storage Engine](04-insane/26-build-database-storage-engine/26-build-database-storage-engine.md) | Insane |
| 27 | [Build a Container Runtime](04-insane/27-build-container-runtime/27-build-container-runtime.md) | Insane |
| 28 | [Build an HTTP/2 Implementation](04-insane/28-build-http2-implementation/28-build-http2-implementation.md) | Insane |
| 29 | [CP: Suffix Array and LCP](04-insane/29-cp-suffix-array-and-lcp/29-cp-suffix-array-and-lcp.md) | Insane |
| 30 | [CP: Heavy-Light Decomposition](04-insane/30-cp-heavy-light-decomposition/30-cp-heavy-light-decomposition.md) | Insane |
| 31 | [CP: Persistent Data Structures](04-insane/31-cp-persistent-data-structures/31-cp-persistent-data-structures.md) | Insane |
| 32 | [CP: Convex Hull Trick](04-insane/32-cp-convex-hull-trick/32-cp-convex-hull-trick.md) | Insane |
| 33 | [Build a Distributed KV Store](04-insane/33-build-distributed-kv-store/33-build-distributed-kv-store.md) | Insane |
| 34 | [Build a Stream Processing Engine](04-insane/34-build-stream-processing-engine/34-build-stream-processing-engine.md) | Insane |
| 35 | [Build a Service Mesh Proxy](04-insane/35-build-service-mesh-proxy/35-build-service-mesh-proxy.md) | Insane |
| 36 | [Build an Interpreter with JIT](04-insane/36-build-interpreter-with-jit/36-build-interpreter-with-jit.md) | Insane |
| 37 | [CP: FFT and Number Theoretic Transform](04-insane/37-cp-fft-number-theoretic-transform/37-cp-fft-number-theoretic-transform.md) | Insane |
| 38 | [Build a TLS 1.3 Handshake](04-insane/38-build-tls-handshake/38-build-tls-handshake.md) | Insane |
| 39 | [Build a Memory-Safe Kernel Module](04-insane/39-build-memory-safe-kernel-module/39-build-memory-safe-kernel-module.md) | Insane |
| 40 | [CP: Centroid Decomposition](04-insane/40-cp-centroid-decomposition/40-cp-centroid-decomposition.md) | Insane |
| 41 | [Build a Profiler](04-insane/41-build-profiler/41-build-profiler.md) | Insane |
| 42 | [Build a CRDT Collaborative Editor](04-insane/42-build-crdt-collaborative-editor/42-build-crdt-collaborative-editor.md) | Insane |
| 43 | [CP: Link-Cut Trees](04-insane/43-cp-link-cut-trees/43-cp-link-cut-trees.md) | Insane |
| 44 | [Build a Regex Engine](04-insane/44-build-regex-engine/44-build-regex-engine.md) | Insane |
| 45 | [Build a Message Queue](04-insane/45-build-message-queue/45-build-message-queue.md) | Insane |
