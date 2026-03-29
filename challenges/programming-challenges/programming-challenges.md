# Programming Challenges — Go & Rust

> 150 standalone systems programming challenges at intermediate-advanced,
> advanced, and insane difficulty levels. Each challenge includes a problem
> description (README.md) and complete solutions (SOLUTION.md) in Go and/or Rust.

**Difficulty Distribution**:
- **Intermediate-Advanced** (45) — Hints + partial scaffolding, 2-4h each
- **Advanced** (60) — Problem + hints only, 4-8h each
- **Insane** (45) — Problem statement only, 8-20h each

**Languages**: Go 1.22+, Rust (stable)

**Categories**: Concurrency, Algorithms, Data Structures, Databases, Compilers, Networking, Network Protocols, Distributed Systems, Cryptography, Systems Programming, Parsers, WebServers, Observability, Compression, Search, Testing, Audio, Video, GameDev, Embedded, ML, Graphics, HPC, Security, Containers, Functional, Verification

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
| 51 | [Huffman Encoding Tree](01-intermediate-advanced/51-huffman-encoding-tree/) | Rust | Compression | 3h |
| 52 | [Inverted Index Text Search](01-intermediate-advanced/52-inverted-index-text-search/) | Go + Rust | Search | 4h |
| 53 | [Cron Expression Parser](01-intermediate-advanced/53-cron-expression-parser/) | Go | Parsers | 3h |
| 54 | [Bounded MPMC Channel](01-intermediate-advanced/54-bounded-mpmc-channel/) | Rust | Concurrency | 4h |
| 55 | [Trie Autocomplete Engine](01-intermediate-advanced/55-trie-autocomplete-engine/) | Go + Rust | Data Structures | 3h |
| 56 | [HTTP Router Radix Tree](01-intermediate-advanced/56-http-router-radix-tree/) | Go | WebServers | 3h |
| 57 | [Property-Based Test Framework](01-intermediate-advanced/57-property-based-test-framework/) | Rust | Testing | 4h |
| 58 | [Circuit Breaker State Machine](01-intermediate-advanced/58-circuit-breaker-state-machine/) | Go | Observability | 3h |
| 59 | [WAV Audio Synthesizer](01-intermediate-advanced/59-wav-audio-synthesizer/) | Rust | Audio | 4h |
| 60 | [Dijkstra Priority Queue](01-intermediate-advanced/60-dijkstra-priority-queue/) | Go + Rust | Algorithms | 2h |
| 61 | [Slab Allocator Fixed Size](01-intermediate-advanced/61-slab-allocator-fixed-size/) | Rust | Systems | 3h |
| 62 | [Middleware Pipeline HTTP](01-intermediate-advanced/62-middleware-pipeline-http/) | Go | WebServers | 3h |
| 63 | [Structured Logger Sink](01-intermediate-advanced/63-structured-logger-sink/) | Go + Rust | Observability | 3h |
| 64 | [Consistent Hashing Ring](01-intermediate-advanced/64-consistent-hashing-ring/) | Go | Distributed | 3h |
| 65 | [LZ77 Compression Sliding Window](01-intermediate-advanced/65-lz77-compression-sliding-window/) | Rust | Compression | 4h |
| 66 | [Semaphore Counting Async](01-intermediate-advanced/66-semaphore-counting-async/) | Rust | Concurrency | 3h |
| 67 | [Entity Component System](01-intermediate-advanced/67-ecs-entity-component-system/) | Rust | GameDev | 4h |
| 68 | [Matrix Multiplication Strassen](01-intermediate-advanced/68-matrix-multiplication-strassen/) | Rust | Algorithms | 3h |
| 69 | [OpenTelemetry Span Collector](01-intermediate-advanced/69-opentelemetry-span-collector/) | Go | Observability | 4h |
| 70 | [Graph BFS/DFS Shortest Path](01-intermediate-advanced/70-graph-bfs-dfs-shortest-path/) | Go + Rust | Algorithms | 2h |
| 71 | [Embedded Sensor Pipeline no_std](01-intermediate-advanced/71-embedded-sensor-pipeline-nostd/) | Rust | Embedded | 4h |
| 72 | [CSV Parser Streaming](01-intermediate-advanced/72-csv-parser-streaming/) | Go + Rust | Parsers | 3h |
| 73 | [SHA-256 Hash from Scratch](01-intermediate-advanced/73-sha256-hash-from-scratch/) | Rust | Crypto | 4h |
| 74 | [Work Stealing Deque](01-intermediate-advanced/74-work-stealing-deque/) | Go | Concurrency | 4h |
| 75 | [HTTP Static File Server](01-intermediate-advanced/75-http-static-file-server/) | Go | WebServers | 3h |
| 76 | [Interval Tree Overlap Queries](01-intermediate-advanced/76-interval-tree-overlap-queries/) | Rust | Data Structures | 3h |
| 77 | [Fuzzer Random Input Generator](01-intermediate-advanced/77-fuzzer-random-input-generator/) | Rust | Security | 4h |
| 78 | [Union-Find Disjoint Sets](01-intermediate-advanced/78-union-find-disjoint-sets/) | Go + Rust | Algorithms | 2h |
| 79 | [Bitcask Log-Structured Store](01-intermediate-advanced/79-bitcask-log-structured-store/) | Go | Databases | 4h |
| 80 | [Functional Option and Result Types](01-intermediate-advanced/80-functional-option-result-types/) | Rust | Functional | 3h |

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
| 81 | [QUIC Transport Handshake](02-advanced/81-quic-transport-handshake/) | Rust | Network Protocols | 8h |
| 82 | [LSM-Tree Compaction Engine](02-advanced/82-lsm-tree-compaction-engine/) | Rust | Databases | 8h |
| 83 | [Gossip Protocol Membership](02-advanced/83-gossip-protocol-membership/) | Go | Distributed | 6h |
| 84 | [Garbage Collector Mark-Sweep](02-advanced/84-garbage-collector-mark-sweep/) | Rust | Compilers | 8h |
| 85 | [BM25 Search Ranking](02-advanced/85-bm25-search-ranking/) | Go + Rust | Search | 6h |
| 86 | [Neural Network Forward Pass](02-advanced/86-neural-network-forward-pass/) | Rust | ML | 6h |
| 87 | [DEFLATE Compression Full](02-advanced/87-deflate-compression-full/) | Rust | Compression | 8h |
| 88 | [TCP State Machine Userspace](02-advanced/88-tcp-state-machine-userspace/) | Rust | Network Protocols | 8h |
| 89 | [Physics Engine 2D Collision](02-advanced/89-physics-engine-2d-collision/) | Rust | GameDev | 6h |
| 90 | [Distributed Tracing Propagation](02-advanced/90-distributed-tracing-propagation/) | Go | Observability | 6h |
| 91 | [TLS Handshake Educational](02-advanced/91-tls-handshake-educational/) | Rust | Security | 8h |
| 92 | [Read-Copy-Update (RCU)](02-advanced/92-read-copy-update-rcu/) | Rust | Concurrency | 6h |
| 93 | [Epoll Event Loop Reactor](02-advanced/93-epoll-event-loop-reactor/) | Rust | Systems | 8h |
| 94 | [Cgroup Resource Limiter](02-advanced/94-cgroup-resource-limiter/) | Go | Containers | 6h |
| 95 | [Video Frame Decoder MJPEG](02-advanced/95-video-frame-decoder-mjpeg/) | Rust | Video | 8h |
| 96 | [gRPC Framework Protobuf](02-advanced/96-grpc-framework-protobuf/) | Go + Rust | Networking | 8h |
| 97 | [A* Pathfinding Grid](02-advanced/97-a-star-pathfinding-grid/) | Go + Rust | Algorithms | 4h |
| 98 | [Time-Series Database Engine](02-advanced/98-time-series-database-engine/) | Go + Rust | Databases | 8h |
| 99 | [Operational Transform Collab](02-advanced/99-operational-transform-collab/) | Go | Distributed | 6h |
| 100 | [Coverage-Guided Fuzzer](02-advanced/100-coverage-guided-fuzzer/) | Rust | Security | 8h |
| 101 | [MVCC Snapshot Isolation](02-advanced/101-mvcc-snapshot-isolation/) | Rust | Databases | 8h |
| 102 | [HTTP Load Balancer Weighted](02-advanced/102-http-load-balancer-weighted/) | Go | WebServers | 6h |
| 103 | [WASM Interpreter Core](02-advanced/103-wasm-interpreter-core/) | Rust | Compilers | 8h |
| 104 | [Namespace Isolation Sandbox](02-advanced/104-namespace-isolation-sandbox/) | Go | Containers | 6h |
| 105 | [Suffix Array Construction](02-advanced/105-suffix-array-construction/) | Rust | Algorithms | 6h |
| 106 | [Audio FFT Spectrum Analyzer](02-advanced/106-audio-fft-spectrum-analyzer/) | Rust | Audio | 6h |
| 107 | [SWIM Failure Detector](02-advanced/107-swim-failure-detector/) | Go | Distributed | 6h |
| 108 | [Hazard Pointer Reclamation](02-advanced/108-hazard-pointer-reclamation/) | Rust | Concurrency | 8h |
| 109 | [Gradient Descent Optimizer](02-advanced/109-gradient-descent-optimizer/) | Rust | ML | 6h |
| 110 | [Embedded RTOS Task Scheduler](02-advanced/110-embedded-rtos-task-scheduler/) | Rust | Embedded | 6h |
| 111 | [Reverse Proxy TLS Termination](02-advanced/111-reverse-proxy-tls-termination/) | Go | WebServers | 6h |
| 112 | [R-Tree Spatial Index](02-advanced/112-r-tree-spatial-index/) | Rust | Data Structures | 6h |
| 113 | [Metrics Pipeline Aggregation](02-advanced/113-metrics-pipeline-aggregation/) | Go + Rust | Observability | 6h |
| 114 | [Log-Structured Merge WAL](02-advanced/114-log-structured-merge-wal/) | Go | Databases | 6h |
| 115 | [CRDTs State-Based Counters](02-advanced/115-crdts-state-based-counters/) | Go + Rust | Distributed | 6h |
| 116 | [Monad Transformer Stack](02-advanced/116-monad-transformer-stack/) | Rust | Functional | 6h |
| 117 | [Model Checker State Explorer](02-advanced/117-model-checker-state-explorer/) | Rust | Verification | 8h |
| 118 | [DNS Authoritative Server](02-advanced/118-dns-authoritative-server/) | Go | Network Protocols | 6h |
| 119 | [Arena Allocator Generational](02-advanced/119-arena-allocator-generational/) | Rust | Systems | 6h |
| 120 | [Convolution Neural Layer](02-advanced/120-convolution-neural-layer/) | Rust | ML | 8h |

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
| 121 | [Full-Text Search Engine](03-insane/121-full-text-search-engine/) | Go + Rust | Search | 20h |
| 122 | [Container Runtime from Scratch](03-insane/122-container-runtime-from-scratch/) | Go | Containers | 20h |
| 123 | [TCP/IP Stack Userspace](03-insane/123-tcp-ip-stack-userspace/) | Rust | Network Protocols | 20h |
| 124 | [Neural Network Training Backprop](03-insane/124-neural-network-training-backprop/) | Rust | ML | 16h |
| 125 | [Video Codec H.264 Baseline](03-insane/125-video-codec-h264-baseline/) | Rust | Video | 20h |
| 126 | [Game Engine 2D Complete](03-insane/126-game-engine-2d-complete/) | Rust | GameDev | 20h |
| 127 | [HTTP Server Framework Full](03-insane/127-http-server-framework-full/) | Go | WebServers | 16h |
| 128 | [Gzip Compressor/Decompressor](03-insane/128-gzip-compressor-decompressor/) | Rust | Compression | 16h |
| 129 | [Symbolic Execution Engine](03-insane/129-symbolic-execution-engine/) | Rust | Verification | 16h |
| 130 | [Embedded Database NoSQL](03-insane/130-embedded-database-nosql/) | Rust | Databases | 20h |
| 131 | [Vector Clock Causal Broadcast](03-insane/131-vector-clock-causal-broadcast/) | Go | Distributed | 16h |
| 132 | [Audio Synthesis DAW Engine](03-insane/132-audio-synthesis-daw-engine/) | Rust | Audio | 16h |
| 133 | [AFL-Style Mutation Fuzzer](03-insane/133-afl-style-mutation-fuzzer/) | Rust | Security | 16h |
| 134 | [Garbage Collector Generational](03-insane/134-garbage-collector-generational/) | Rust | Compilers | 20h |
| 135 | [QUIC Reliable Streams Full](03-insane/135-quic-reliable-streams-full/) | Rust | Network Protocols | 20h |
| 136 | [Distributed Log Kafka-Style](03-insane/136-distributed-log-kafka-style/) | Go + Rust | Distributed | 20h |
| 137 | [Query Optimizer Cost-Based](03-insane/137-query-optimizer-cost-based/) | Rust | Databases | 16h |
| 138 | [Embedded RTOS Kernel no_std](03-insane/138-embedded-rtos-kernel-nostd/) | Rust | Embedded | 20h |
| 139 | [Register Allocator Graph Coloring](03-insane/139-register-allocator-graph-coloring/) | Rust | Compilers | 16h |
| 140 | [Linker ELF Object Files](03-insane/140-linker-elf-object-files/) | Rust | Systems | 20h |
| 141 | [Observability Platform Pipeline](03-insane/141-observability-platform-pipeline/) | Go + Rust | Observability | 16h |
| 142 | [Peer-to-Peer DHT Kademlia](03-insane/142-peer-to-peer-dht-kademlia/) | Go | Distributed | 16h |
| 143 | [CNN Inference Engine Optimized](03-insane/143-cnn-inference-engine-optimized/) | Rust | ML | 20h |
| 144 | [Software Rasterizer 3D](03-insane/144-software-rasterizer-3d/) | Rust | Graphics | 16h |
| 145 | [Property Testing Shrinking Engine](03-insane/145-property-testing-shrinking-engine/) | Go + Rust | Testing | 12h |
| 146 | [Effect System Algebraic](03-insane/146-effect-system-algebraic/) | Rust | Functional | 16h |
| 147 | [Service Mesh Sidecar Proxy](03-insane/147-service-mesh-sidecar-proxy/) | Go | Networking | 16h |
| 148 | [Persistent Data Structures Immutable](03-insane/148-persistent-data-structures-immutable/) | Rust | Functional | 12h |
| 149 | [Chaos Engineering Fault Injector](03-insane/149-chaos-engineering-fault-injector/) | Go | Distributed | 16h |
| 150 | [Optimizing Compiler SSA Form](03-insane/150-optimizing-compiler-ssa-form/) | Rust | Compilers | 20h |

---

## By Category

| Category | Challenges |
|----------|-----------|
| Algorithms | 04, 14, 60, 68, 70, 78, 97, 105 |
| Audio | 59, 106, 132 |
| Compilers | 23, 24, 25, 33, 36, 37, 49, 84, 103, 134, 139, 150 |
| Compression | 51, 65, 87, 128 |
| Concurrency | 01, 09, 11, 16, 17, 54, 66, 74, 92, 108 |
| Containers | 94, 104, 122 |
| Crypto | 28, 29, 45, 73 |
| Data Structures | 02, 03, 12, 15, 32, 55, 76, 112 |
| Databases | 18, 19, 21, 35, 44, 79, 82, 98, 101, 114, 130, 137 |
| Distributed | 20, 34, 38, 39, 47, 48, 50, 64, 83, 99, 107, 115, 131, 136, 142, 149 |
| Embedded | 71, 110, 138 |
| Functional | 80, 116, 146, 148 |
| GameDev | 67, 89, 126 |
| Graphics | 40, 144 |
| HPC | 46 |
| ML | 86, 109, 120, 124, 143 |
| Network Protocols | 81, 88, 118, 123, 135 |
| Networking | 05, 10, 13, 26, 27, 30, 96, 147 |
| Observability | 58, 63, 69, 90, 113, 141 |
| OS | 41, 42, 43 |
| Parsers | 06, 07, 08, 53, 72 |
| Search | 52, 85, 121 |
| Security | 77, 91, 100, 133 |
| Systems | 22, 31, 61, 93, 119, 140 |
| Testing | 57, 145 |
| Verification | 117, 129 |
| Video | 95, 125 |
| WebServers | 56, 62, 75, 102, 111, 127 |

## By Language

| Language | Count | Challenges |
|----------|-------|-----------|
| Go only | 39 | 01, 05, 09, 10, 11, 20, 26, 27, 30, 34, 38, 39, 47, 48, 53, 56, 58, 62, 64, 69, 74, 75, 79, 83, 90, 94, 99, 102, 104, 107, 111, 114, 118, 122, 127, 131, 142, 147, 149 |
| Rust only | 84 | 03, 06, 08, 12, 14, 15, 16, 17, 18, 21, 22, 24, 28, 29, 31, 33, 36, 37, 40, 41, 42, 43, 44, 45, 46, 49, 51, 54, 57, 59, 61, 65, 66, 67, 68, 71, 73, 76, 77, 80, 81, 82, 84, 86, 87, 88, 89, 91, 92, 93, 95, 100, 101, 103, 105, 106, 108, 109, 110, 112, 116, 117, 119, 120, 123, 124, 125, 126, 128, 129, 130, 132, 133, 134, 135, 137, 138, 139, 140, 143, 144, 146, 148, 150 |
| Go + Rust | 27 | 02, 04, 07, 13, 19, 23, 25, 32, 35, 50, 52, 55, 60, 63, 70, 72, 78, 85, 96, 97, 98, 113, 115, 121, 136, 141, 145 |
