# Programming Challenges — Go & Rust

> 50 standalone systems programming challenges at intermediate-advanced,
> advanced, and insane difficulty levels. Each challenge includes a problem
> description (README.md) and complete solutions (SOLUTION.md) in Go and/or Rust.

**Difficulty Distribution**:
- **Intermediate-Advanced** (15) — Hints + partial scaffolding, 2-4h each
- **Advanced** (20) — Problem + hints only, 4-8h each
- **Insane** (15) — Problem statement only, 8-20h each

**Languages**: Go 1.22+, Rust (stable)

**Categories**: Concurrency, Algorithms, Data Structures, Databases, Compilers, Networking, Distributed Systems, Cryptography, Systems Programming

---

## 01 — Intermediate-Advanced

| # | Challenge | Languages | Category | Est. Time |
|---|-----------|-----------|----------|-----------|
| 01 | [Backpressure-Aware Producer-Consumer Pipeline](01-intermediate-advanced/01-backpressure-producer-consumer/) | Go | Concurrency | 3h |
| 02 | [Thread-Safe LRU Cache with TTL Expiration](01-intermediate-advanced/02-thread-safe-lru-cache-ttl/) | Go + Rust | Data Structures | 3h |
| 03 | [Bloom Filter with Dynamic Resizing](01-intermediate-advanced/03-bloom-filter-dynamic-resizing/) | Rust | Data Structures | 3h |
| 04 | [Topological Sort with Cycle Detection](01-intermediate-advanced/04-topological-sort-cycle-detection/) | Go + Rust | Algorithms | 2h |
| 05 | [Custom DNS Resolver with Caching](01-intermediate-advanced/05-custom-dns-resolver-caching/) | Go | Networking | 4h |
| 06 | [JSON Parser from Scratch](01-intermediate-advanced/06-json-parser-from-scratch/) | Rust | Parsers | 4h |
| 07 | [SQL SELECT Statement Parser](01-intermediate-advanced/07-sql-select-parser/) | Go + Rust | Parsers | 4h |
| 08 | [Expression Evaluator with Operator Precedence](01-intermediate-advanced/08-expression-evaluator-precedence/) | Rust | Parsers | 3h |
| 09 | [Concurrent State Machine with Validation](01-intermediate-advanced/09-concurrent-state-machine/) | Go | Concurrency | 3h |
| 10 | [HTTP/1.1 Client with Connection Pooling](01-intermediate-advanced/10-http-client-connection-pooling/) | Go | Networking | 4h |
| 11 | [Priority Work Queue with Starvation Prevention](01-intermediate-advanced/11-priority-work-queue-starvation/) | Go | Concurrency | 3h |
| 12 | [Skip List Implementation](01-intermediate-advanced/12-skip-list-implementation/) | Rust | Data Structures | 4h |
| 13 | [Rate-Limited Socket with Token Bucket](01-intermediate-advanced/13-rate-limited-socket-token-bucket/) | Go + Rust | Networking | 3h |
| 14 | [Edit Distance with Custom Operations](01-intermediate-advanced/14-edit-distance-custom-operations/) | Rust | Algorithms | 2h |
| 15 | [Ring Buffer with Zero-Copy Semantics](01-intermediate-advanced/15-ring-buffer-zero-copy/) | Rust | Data Structures | 3h |

---

## 02 — Advanced

| # | Challenge | Languages | Category | Est. Time |
|---|-----------|-----------|----------|-----------|
| 16 | [Lock-Free Stack (Treiber's Algorithm)](02-advanced/16-lock-free-stack-treiber/) | Rust | Concurrency | 6h |
| 17 | [Lock-Free Queue (Michael-Scott Algorithm)](02-advanced/17-lock-free-queue-michael-scott/) | Rust | Concurrency | 6h |
| 18 | [B+ Tree Index for Disk Storage](02-advanced/18-bplus-tree-disk-storage/) | Rust | Databases | 8h |
| 19 | [Write-Ahead Log (WAL) Manager](02-advanced/19-write-ahead-log-manager/) | Go + Rust | Databases | 6h |
| 20 | [Raft Consensus Leader Election](02-advanced/20-raft-consensus-leader-election/) | Go | Distributed | 8h |
| 21 | [Page-Based Storage Engine](02-advanced/21-page-based-storage-engine/) | Rust | Databases | 6h |
| 22 | [Custom Memory Pool Allocator](02-advanced/22-custom-memory-pool-allocator/) | Rust | Systems | 6h |
| 23 | [Recursive Descent Parser + AST Builder](02-advanced/23-recursive-descent-parser-ast/) | Go + Rust | Compilers | 6h |
| 24 | [Type Checker for Simple Language](02-advanced/24-type-checker-simple-language/) | Rust | Compilers | 8h |
| 25 | [Stack-Based Virtual Machine](02-advanced/25-stack-based-virtual-machine/) | Go + Rust | Compilers | 6h |
| 26 | [WebSocket Server Protocol Implementation](02-advanced/26-websocket-server-protocol/) | Go | Networking | 6h |
| 27 | [HTTP/2 Stream Multiplexer](02-advanced/27-http2-stream-multiplexer/) | Go | Networking | 8h |
| 28 | [Merkle Tree with Inclusion Proofs](02-advanced/28-merkle-tree-inclusion-proofs/) | Rust | Crypto | 4h |
| 29 | [AES Block Cipher (Educational)](02-advanced/29-aes-block-cipher-educational/) | Rust | Crypto | 6h |
| 30 | [Connection Pool with Health Checking](02-advanced/30-connection-pool-health-checking/) | Go | Networking | 4h |
| 31 | [Streaming Decompression Pipeline](02-advanced/31-streaming-decompression-pipeline/) | Rust | Systems | 6h |
| 32 | [Concurrent B-Tree with Fine-Grained Locking](02-advanced/32-concurrent-btree-fine-grained/) | Go + Rust | Data Structures | 8h |
| 33 | [Custom Regex Engine (NFA to DFA)](02-advanced/33-custom-regex-engine-nfa-dfa/) | Rust | Compilers | 8h |
| 34 | [2-Phase Commit Coordinator](02-advanced/34-two-phase-commit-coordinator/) | Go | Distributed | 6h |
| 35 | [Lock Manager with Deadlock Detection](02-advanced/35-lock-manager-deadlock-detection/) | Go + Rust | Databases | 6h |

---

## 03 — Insane

| # | Challenge | Languages | Category | Est. Time |
|---|-----------|-----------|----------|-----------|
| 36 | [Complete Language Interpreter](03-insane/36-complete-language-interpreter/) | Rust | Compilers | 16h |
| 37 | [Bytecode Compiler with Optimizations](03-insane/37-bytecode-compiler-optimizations/) | Rust | Compilers | 16h |
| 38 | [Distributed Key-Value Store](03-insane/38-distributed-key-value-store/) | Go | Distributed | 20h |
| 39 | [MapReduce Framework](03-insane/39-mapreduce-framework/) | Go | Distributed | 16h |
| 40 | [Ray Tracing Engine](03-insane/40-ray-tracing-engine/) | Rust | Graphics | 16h |
| 41 | [Custom File System (In-Memory)](03-insane/41-custom-file-system-in-memory/) | Rust | OS | 20h |
| 42 | [Thread Scheduler Simulation](03-insane/42-thread-scheduler-simulation/) | Rust | OS | 16h |
| 43 | [Heap Memory Allocator (malloc)](03-insane/43-heap-memory-allocator-malloc/) | Rust | OS | 20h |
| 44 | [Columnar Database Engine](03-insane/44-columnar-database-engine/) | Rust | Databases | 20h |
| 45 | [Zero-Knowledge Proof System](03-insane/45-zero-knowledge-proof-system/) | Rust | Crypto | 16h |
| 46 | [SIMD-Optimized Matrix Operations](03-insane/46-simd-optimized-matrix-ops/) | Rust | HPC | 12h |
| 47 | [Event-Driven Pub/Sub with Durability](03-insane/47-event-driven-pubsub-durability/) | Go | Distributed | 16h |
| 48 | [Consensus-Based Replicated State Machine](03-insane/48-consensus-replicated-state-machine/) | Go | Distributed | 20h |
| 49 | [JIT Compiler Backend](03-insane/49-jit-compiler-backend/) | Rust | Compilers | 20h |
| 50 | [Distributed SQL Query Engine](03-insane/50-distributed-sql-query-engine/) | Go + Rust | Distributed | 20h |

---

## By Category

| Category | Challenges |
|----------|-----------|
| Concurrency | 01, 09, 11, 16, 17 |
| Data Structures | 02, 03, 12, 15, 32 |
| Algorithms | 04, 14 |
| Parsers & Compilers | 06, 07, 08, 23, 24, 25, 33, 36, 37, 49 |
| Databases | 18, 19, 21, 35, 44 |
| Networking | 05, 10, 13, 26, 27, 30, 31 |
| Distributed Systems | 20, 34, 38, 39, 47, 48, 50 |
| Cryptography | 28, 29, 45 |
| Systems Programming | 22, 40, 41, 42, 43, 46 |

## By Language

| Language | Count | Challenges |
|----------|-------|-----------|
| Go only | 14 | 01, 05, 09, 10, 11, 20, 26, 27, 30, 34, 38, 39, 47, 48 |
| Rust only | 22 | 03, 06, 08, 12, 14, 15, 16, 17, 18, 21, 22, 24, 28, 29, 31, 33, 36, 37, 40, 41, 42, 43, 44, 45, 46, 49 |
| Go + Rust | 14 | 02, 04, 07, 13, 19, 23, 25, 32, 35, 50 |
