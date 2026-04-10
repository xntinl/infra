# Advanced Topics — Reference Documentation for Senior Developers

> Dense reference documentation for engineers who want to understand the internals behind the systems they build. Unlike the other sections in this project (which are progressive exercises), this is a knowledge map: each document is a self-contained deep dive into a concept, with mental models, idiomatic Go and Rust implementations, production war stories, and exercises scaled for senior engineers.

**Prerequisites**: Comfortable with Go or Rust at an intermediate level. Familiarity with data structures and algorithms fundamentals. Completed at least sections 01–30 of `/go/` or the advanced section of `/rust/` is recommended but not required.

**Format**: Each document follows a consistent structure — Mental Model → Core Concepts → Go Implementation → Rust Implementation → Go vs Rust Comparison → Production War Stories → Complexity Analysis → Common Pitfalls → Exercises → Further Reading.

**Time estimates**: Each subtopic document takes 45–90 minutes to read. Each section (overview + all subtopics) takes 6–15 hours for working knowledge, 40–80 hours for mastery.

---

## Sections

| # | Section | Subtopics | Total Reading | Difficulty |
|---|---------|-----------|---------------|------------|
| 01 | [Advanced Data Structures](./01-advanced-data-structures/01-advanced-data-structures.md) | 8 | ~8h | advanced |
| 02 | [Advanced Algorithms](./02-advanced-algorithms/02-advanced-algorithms.md) | 8 | ~8h | advanced |
| 03 | [Concurrency and Parallelism](./03-concurrency-and-parallelism/03-concurrency-and-parallelism.md) | 9 | ~10h | advanced |
| 04 | [Distributed Systems](./04-distributed-systems/04-distributed-systems.md) | 9 | ~10h | advanced |
| 05 | [Compilers and Runtimes](./05-compilers-and-runtimes/05-compilers-and-runtimes.md) | 8 | ~8h | advanced–insane |
| 06 | [Database Internals](./06-database-internals/06-database-internals.md) | 8 | ~8h | advanced |
| 07 | [Systems Programming and OS](./07-systems-and-os/07-systems-and-os.md) | 8 | ~8h | advanced |
| 08 | [Networking and Security](./08-networking-and-security/08-networking-and-security.md) | 8 | ~8h | advanced–insane |
| 09 | [Performance and Optimization](./09-performance-and-optimization/09-performance-and-optimization.md) | 7 | ~7h | advanced |
| 10 | [Metaprogramming](./10-metaprogramming/10-metaprogramming.md) | 7 | ~7h | advanced |
| 11 | [Software Architecture](./11-software-architecture/11-software-architecture.md) | 8 | ~8h | advanced |
| 12 | [Type Theory and FP](./12-type-theory-and-fp/12-type-theory-and-fp.md) | 7 | ~8h | insane |

**Total**: 12 sections · 95 subtopics · ~100h reading · ~800h mastery

---

## Section Dependency Map

Some sections build on concepts from others. This map helps you choose a reading order:

```
01-data-structures ──→ 03-concurrency (lock-free uses skip list)
01-data-structures ──→ 06-database (B-tree, LSM, bloom filters)
02-algorithms      ──→ 05-compilers (SSA, dataflow analysis)
03-concurrency     ──→ 05-compilers (GC algorithms, async runtimes)
03-concurrency     ──→ 07-systems-os (NUMA, io models)
01-data-structures ──→ 04-distributed (consistent hashing)
05-compilers       ──→ 12-type-theory (type inference)
10-metaprogramming ──→ 12-type-theory (derive macros = type-level programs)
```

**Independence**: Sections 07, 08, 09, 11 can be read in any order without prior sections.

---

## Recommended Reading Paths

### Path A: Systems Engineer
> Focus on performance, OS internals, and distributed systems.
1. 09-performance-and-optimization
2. 07-systems-and-os
3. 03-concurrency-and-parallelism
4. 04-distributed-systems
5. 06-database-internals

### Path B: Language/Compiler Engineer
> Focus on how languages work internally.
1. 01-advanced-data-structures
2. 02-advanced-algorithms
3. 03-concurrency-and-parallelism
4. 05-compilers-and-runtimes
5. 12-type-theory-and-fp
6. 10-metaprogramming

### Path C: Security Engineer
> Focus on cryptography, protocols, and secure systems.
1. 08-networking-and-security
2. 07-systems-and-os (eBPF, kernel bypass)
3. 04-distributed-systems (BFT)
4. 12-type-theory-and-fp (linear types for resource safety)

### Path D: Full Coverage (Sequential)
> Read sections in numbered order — each builds on the previous.
01 → 02 → 03 → 04 → 05 → 06 → 07 → 08 → 09 → 10 → 11 → 12

---

## How to Use This Section

Each subtopic document is a **reference**, not a tutorial. The intended usage pattern:

1. **Read the Mental Model first** (~5 min) — decide if this is worth your time now
2. **Read Core Concepts + one implementation** (~30–45 min) — build working knowledge
3. **Study the Go vs Rust comparison** (~15 min) — understand the tradeoffs
4. **Read the Production War Stories** (~10 min) — see why it matters
5. **Complete Exercise 1–2** (~2–6h) — validate understanding
6. **Return to Further Reading** (ongoing) — go deep on what interests you

You do not need to read all of a document in one sitting. The structure is designed for reference lookup.

---

## Languages

All code examples are in **Go** and **Rust**. Examples are:
- Complete and standalone (runnable with `go run .` or `cargo run`)
- Idiomatic for each language (not direct translations)
- Annotated with design rationale, not syntax explanations
- Production-grade (error handling included, not elided)

For Go examples: requires Go 1.21+
For Rust examples: requires Rust 1.75+ (stable, for GATs and const generics)
