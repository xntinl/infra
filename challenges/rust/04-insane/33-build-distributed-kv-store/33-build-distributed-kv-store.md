# 33. Build a Distributed KV Store

**Difficulty**: Insane

## The Challenge

Building a distributed key-value store from scratch is one of the most demanding systems programming challenges because it sits at the intersection of storage engine design, distributed consensus, network programming, and concurrent systems. Your task is to build a fully functional distributed key-value store in Rust that provides linearizable reads and writes, automatic replication via the Raft consensus protocol, persistent storage using an LSM-tree engine, and horizontal scalability through consistent hashing-based sharding. This is not a toy project — you are building something architecturally similar to TiKV or etcd, albeit scoped to a manageable feature set.

The storage engine alone is a significant undertaking. You will implement a Log-Structured Merge-tree (LSM-tree) with an in-memory memtable (backed by a skip list or B-tree), write-ahead logging for crash recovery, immutable Sorted String Tables (SSTables) flushed to disk, and a multi-level compaction strategy to reclaim space and maintain read performance. On top of this, you must layer the Raft consensus protocol to replicate operations across a cluster of nodes, ensuring that a committed write is durable even if a minority of nodes crash. The Raft implementation must handle leader election, log replication, snapshotting, and membership changes. Nodes communicate via gRPC (using tonic), and a routing layer distributes keys across shards using consistent hashing with virtual nodes.

The true difficulty lies in composing these pieces correctly under concurrent access, partial failures, and network partitions. A client write must be proposed to the Raft leader, replicated to a quorum, applied to the state machine (your LSM-tree), and acknowledged to the client — all while other writes are in flight, background compaction is running, and a node might be recovering from a crash by replaying its write-ahead log and catching up on missed Raft entries. You must reason about linearizability (can a read ever return stale data after a write was acknowledged?), exactly-once semantics (what if the client retries a timed-out write that actually succeeded?), and liveness (does your system make progress if exactly one node in a three-node cluster is unreachable?). This challenge will push your understanding of Rust's ownership model, async programming, and low-level systems design to their absolute limits.

## Acceptance Criteria

### Storage Engine: LSM-Tree

- [ ] Implement an in-memory **memtable** backed by a concurrent skip list or `BTreeMap`
  - Support `put(key: Vec<u8>, value: Vec<u8>)` — insert or update
  - Support `get(key: &[u8]) -> Option<Vec<u8>>` — point lookup
  - Support `delete(key: &[u8])` — insert a tombstone marker
  - Support `scan(start: &[u8], end: &[u8]) -> Iterator` — range scan in sorted order
  - Thread-safe: multiple readers and a single writer (or multiple writers with proper synchronization)
  - Track approximate memory usage; trigger flush when memtable exceeds a configurable threshold (e.g., 4 MB)

- [ ] Implement a **Write-Ahead Log (WAL)**
  - Every write operation is appended to the WAL before being applied to the memtable
  - WAL entries are length-prefixed and CRC32-checksummed for corruption detection
  - On crash recovery, replay the WAL to reconstruct the memtable
  - Support WAL truncation after a successful memtable flush to SSTable
  - WAL is append-only and uses `fsync` (via `File::sync_all()`) for durability

- [ ] Implement **SSTable** (Sorted String Table) file format
  - Data block: sorted key-value pairs with prefix compression
  - Index block: sparse index mapping key prefixes to data block offsets
  - Bloom filter block: probabilistic filter to avoid unnecessary disk reads for missing keys
  - Footer: offsets to index block and bloom filter block, plus a magic number for format validation
  - Immutable once written: SSTables are never modified, only created and deleted
  - Support reading a single key via index lookup + bloom filter check + data block scan

- [ ] Implement **multi-level compaction**
  - L0: flushed memtables land here, may have overlapping key ranges
  - L1+: non-overlapping key ranges within each level, exponentially increasing size limits
  - Compaction merges SSTables from level L with overlapping SSTables in level L+1
  - Tombstones are propagated downward and only dropped when they reach the bottom level
  - Compaction runs in a background thread/task and does not block reads or writes
  - Implement at least leveled compaction (size-tiered compaction is optional)

- [ ] Implement the **read path** across all levels
  - Check memtable first, then immutable memtable (if a flush is in progress), then L0 SSTables (newest first), then L1+ SSTables
  - Use bloom filters to skip SSTables that definitely do not contain the key
  - Use the sparse index to binary-search within a candidate SSTable
  - A tombstone found at any level means the key is deleted — do not check lower levels
  - Range scans merge iterators from all levels using a merge-sort-like approach

### Raft Consensus Protocol

- [ ] Implement the **Raft leader election** protocol
  - Nodes start as followers with a randomized election timeout (150-300ms)
  - On timeout, a follower becomes a candidate and requests votes from all peers
  - A candidate wins if it receives votes from a majority of nodes
  - Implement the RequestVote RPC: grant vote only if candidate's log is at least as up-to-date
  - At most one leader per term — enforce via `voted_for` persistent state
  - Leader sends periodic heartbeats (empty AppendEntries) to prevent new elections

- [ ] Implement **log replication** via AppendEntries RPC
  - Leader appends client writes to its log and sends AppendEntries to all followers
  - Followers append entries if `prev_log_index` and `prev_log_term` match, otherwise reject
  - Leader retries with decremented `next_index` on rejection (log backtracking)
  - An entry is **committed** once replicated to a majority of nodes
  - Committed entries are applied to the state machine (LSM-tree) in log order
  - Leader tracks `commit_index` and communicates it to followers

- [ ] Implement **Raft persistent state**
  - `current_term`, `voted_for`, and the log entries must survive restarts
  - Store Raft state in a separate file or embedded database (not in the LSM-tree)
  - On startup, load persistent state before participating in elections
  - Use `fsync` after every state mutation that must be durable before responding

- [ ] Implement **Raft snapshots** for log compaction
  - Periodically snapshot the state machine (LSM-tree) and discard log entries up to the snapshot index
  - Implement InstallSnapshot RPC for slow followers that have fallen too far behind
  - The snapshot includes the full key-value state at a specific log index
  - After receiving a snapshot, a follower replaces its state machine and adjusts its log

- [ ] Implement **read linearizability**
  - Option A (ReadIndex): leader confirms it is still leader by sending a heartbeat round before serving the read
  - Option B (Lease-based): leader holds a time-based lease; reads served locally within the lease period
  - Implement at least one of these approaches and document the trade-offs
  - Include a test demonstrating that stale reads do NOT occur after a leadership change

### Networking and gRPC

- [ ] Define gRPC service definitions using protobuf (tonic + prost)
  - `RaftService`: `RequestVote`, `AppendEntries`, `InstallSnapshot` RPCs
  - `KVService`: `Put(key, value) -> PutResponse`, `Get(key) -> GetResponse`, `Delete(key) -> DeleteResponse`, `Scan(start, end) -> stream ScanResponse`
  - Include proper error codes: `NOT_LEADER` (with leader hint), `KEY_NOT_FOUND`, `TIMEOUT`

- [ ] Implement the **KV client** that talks to the cluster
  - Automatically discovers the leader by trying any node and following `NOT_LEADER` redirects
  - Caches the known leader and retries on a different node if the leader becomes unreachable
  - Supports configurable timeout and retry count
  - Assigns a unique `client_id` and monotonic `request_id` for deduplication

- [ ] Implement **request deduplication** on the server side
  - Track the last applied `request_id` per `client_id`
  - If a duplicate request arrives (same client_id + request_id), return the cached response
  - This prevents double-application of writes when a client retries after a timeout
  - Deduplication table is included in Raft snapshots

### Sharding and Routing

- [ ] Implement **consistent hashing** with virtual nodes
  - The key space is mapped to a hash ring using a hash function (e.g., xxHash or SHA-256 truncated)
  - Each physical node owns multiple virtual nodes (configurable, e.g., 128 per physical node)
  - A key is assigned to the first virtual node clockwise on the ring from the key's hash
  - Replication: the key is stored on the next N-1 distinct physical nodes clockwise (N = replication factor)

- [ ] Implement **shard-to-Raft-group mapping**
  - The hash ring is divided into a fixed number of shards (e.g., 64 or 256)
  - Each shard is managed by an independent Raft group
  - A node may participate in multiple Raft groups (one per shard it is responsible for)
  - The routing layer maps a key to its shard, then routes the request to the leader of that shard's Raft group

- [ ] Implement **shard rebalancing** when nodes join or leave
  - When a new node joins, some shards are transferred to it
  - The transfer is coordinated: the old leader stops accepting writes for the shard, sends a snapshot to the new node, and the new node joins the Raft group for that shard
  - When a node leaves (gracefully), it transfers its shards to remaining nodes before shutting down
  - During rebalancing, requests for affected shards receive a `SHARD_MIGRATING` error and the client retries

### Concurrency and Safety

- [ ] All shared state is protected by appropriate synchronization primitives
  - The memtable uses `Arc<RwLock<...>>` or a lock-free concurrent data structure
  - The Raft state machine uses a mutex or channel-based serialization
  - Background tasks (compaction, snapshotting) run in separate tokio tasks and communicate via channels

- [ ] Graceful shutdown
  - On SIGTERM/SIGINT, the node flushes the memtable to disk, syncs the WAL, and notifies peers
  - In-flight RPCs are given a grace period to complete
  - The node deregisters from the cluster membership

- [ ] No `unsafe` code in the application layer
  - Unsafe is permitted only in low-level primitives (e.g., if implementing a custom skip list) with safety comments
  - All cross-thread communication uses Rust's ownership model (channels, Arc, etc.)

### Testing

- [ ] Unit tests for the LSM-tree storage engine
  - Put/get/delete individual keys
  - Range scans return results in sorted order
  - Memtable flush produces a valid SSTable
  - Compaction correctly merges overlapping SSTables and drops stale tombstones at the bottom level
  - WAL replay recovers the memtable after a simulated crash
  - Bloom filter has zero false negatives and a reasonable false positive rate (~1%)
  - Concurrent reads and writes do not produce data races or panics

- [ ] Unit tests for the Raft implementation
  - Leader election with 3 nodes: exactly one leader per term
  - Log replication: write on leader appears on followers after commit
  - Leader failure: follower times out and becomes new leader
  - Network partition: minority partition does not elect a leader; majority partition continues
  - Log conflict resolution: follower with divergent log entries is corrected by the leader
  - Snapshot transfer: slow follower catches up via InstallSnapshot

- [ ] Integration tests for the full distributed system
  - Start a 3-node cluster in-process (using different ports or in-memory networking)
  - Write a key on the leader, read it from any node, verify linearizability
  - Kill the leader, wait for re-election, verify the key is still readable
  - Write 10_000 keys, kill a node, restart it, verify it catches up and has all keys
  - Concurrent writes from multiple clients do not produce lost updates or inconsistencies

- [ ] Chaos testing (optional but strongly encouraged)
  - Randomly kill and restart nodes during a write workload
  - Introduce artificial network delays and partitions
  - Verify that the system remains consistent (no lost or duplicated writes) after the chaos period

### Performance

- [ ] Single-node throughput benchmarks
  - LSM-tree: > 100K writes/sec for 100-byte keys and values (with WAL, without Raft)
  - LSM-tree: > 200K point reads/sec with warm bloom filters
  - Range scan of 1000 consecutive keys in < 1ms

- [ ] Cluster throughput benchmarks (3-node, local)
  - > 10K linearizable writes/sec (limited by Raft round-trip)
  - > 50K linearizable reads/sec (with ReadIndex or lease-based reads)

- [ ] Latency benchmarks
  - p50 write latency < 5ms for a 3-node local cluster
  - p99 write latency < 20ms
  - p50 read latency < 2ms (lease-based) or < 5ms (ReadIndex)

### Code Organization

- [ ] Use a Cargo workspace with separate crates:
  - `kv-store` — the binary crate (main entry point, CLI argument parsing)
  - `storage` — the LSM-tree storage engine
  - `raft` — the Raft consensus implementation
  - `proto` — protobuf definitions and generated code
  - `client` — the KV client library
  - `common` — shared types, error definitions, configuration

- [ ] Configuration via a TOML or YAML file:
  - Node ID, listen address, peer addresses
  - Storage directory path
  - Memtable size threshold, compaction settings
  - Raft election timeout range, heartbeat interval
  - Replication factor, shard count

- [ ] Structured logging using `tracing` crate
  - Log all Raft state transitions (follower -> candidate -> leader)
  - Log compaction events (which SSTables were merged, how much space was reclaimed)
  - Log client request handling (put/get/delete with timing)
  - Use appropriate log levels (debug for routine events, info for state changes, warn for retries, error for failures)

## Starting Points

- **TiKV**: The Rust-based distributed KV store used by TiDB. Study its architecture docs at tikv.org and the `raft-rs` crate it uses. TiKV's design is the gold standard for this type of system.
- **etcd**: Go-based, but its Raft implementation and linearizable read design are extremely well documented. The etcd Raft design doc explains ReadIndex and lease-based reads in detail.
- **mini-lsm**: A Rust-based educational LSM-tree project (github.com/skyzh/mini-lsm) with a step-by-step tutorial. Excellent starting point for the storage engine component.
- **raft-rs**: The standalone Raft library extracted from TiKV (github.com/tikv/raft-rs). You can study it for reference but should implement your own for the learning experience.
- **Designing Data-Intensive Applications** by Martin Kleppmann: Chapters 3 (Storage), 5 (Replication), 6 (Partitioning), and 9 (Consistency) are directly relevant.
- **In Search of an Understandable Consensus Algorithm** (Ongaro & Ousterhout, 2014): The original Raft paper. Read sections 5-7 carefully; they contain all the details you need.
- **tonic**: The Rust gRPC framework (github.com/hyperium/tonic). Use it with prost for code generation from protobuf definitions.
- **LevelDB/RocksDB**: The original LSM-tree implementations. LevelDB's design doc (in its source repo) is a concise description of the SSTable format and compaction algorithm.

## Hints

1. **Start with the storage engine in isolation.** Build and thoroughly test the LSM-tree before adding Raft or networking. The storage engine is the most self-contained component, and bugs here will be nearly impossible to diagnose once you add distributed consensus on top.

2. **Implement Raft against the paper, not against another implementation.** The Raft paper (extended version) includes pseudocode for all RPCs and state transitions in Figure 2. Implement exactly what Figure 2 says, and write tests for each invariant. Most Raft bugs come from subtle deviations from the paper.

3. **Use a tick-based timer for Raft instead of real-time clocks in tests.** Your Raft implementation should accept a `tick()` call that advances its logical clock. In production, a tokio timer calls `tick()` periodically (e.g., every 50ms). In tests, you call `tick()` manually, giving you deterministic control over election timeouts and heartbeats.

4. **The memtable flush and Raft apply must be carefully coordinated.** When the Raft state machine applies a committed entry, the write goes to the memtable. When the memtable is flushed to an SSTable, you must not lose any writes that were applied but not yet flushed. The WAL handles this: it is only truncated after a successful flush, and on recovery, you replay the WAL from the last flush point.

5. **For the consistent hashing ring, use a sorted `Vec<(u64, NodeId)>` and binary search.** This is simpler and faster than a balanced tree for a ring that changes rarely. When looking up a key, hash it to a `u64`, binary search for the first virtual node with a hash >= the key's hash (wrapping around if necessary), and that's the owning node.

6. **Linearizable reads are surprisingly tricky.** A naive "read from the leader" is NOT linearizable because the leader might have been deposed without knowing it. The ReadIndex approach works: when a read arrives, the leader records the current commit index, sends a heartbeat to confirm it's still leader, and then waits for the commit index to be applied before serving the read. This ensures the read reflects all committed writes.

7. **Request deduplication is essential for exactly-once semantics.** Without it, a client that retries a timed-out write (which actually succeeded) will apply the write twice. Store a `HashMap<ClientId, (RequestId, Response)>` in the state machine, and include it in snapshots. Expire entries after a configurable TTL to bound memory usage.

8. **Use `tokio::sync::mpsc` channels to serialize access to Raft state.** Rather than locking the Raft state machine directly, send commands (Propose, Tick, ReceivedMessage) to a single "Raft driver" task that processes them sequentially. This eliminates deadlocks and makes the code much easier to reason about.

9. **For SSTable file I/O, memory-map the files using `memmap2` for read performance.** This lets the OS manage the page cache and avoids copying data into userspace buffers. Be aware of the safety considerations: the file must not be truncated while mapped, and you should handle I/O errors (which manifest as SIGBUS on Linux).

10. **Test with deterministic simulations before testing with real networking.** Create an in-memory networking layer where messages between nodes are delivered through channels, with configurable delays, reordering, and drops. This lets you write reproducible tests for network partition scenarios. Only switch to real gRPC for final integration testing.
