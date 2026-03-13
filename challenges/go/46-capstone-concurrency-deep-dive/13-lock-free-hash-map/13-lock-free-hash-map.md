<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3.5h
-->

# Lock-Free Hash Map

## The Challenge

Implement a lock-free concurrent hash map that supports concurrent reads, inserts, updates, and deletes without any locks, achieving scalable throughput that increases linearly with the number of cores. Unlike `sync.Map` which uses a combination of atomic operations and a mutex-protected dirty map, your implementation must be fully lock-free using open addressing with Robin Hood hashing for probe sequence optimization, and must support dynamic resizing (growing and shrinking) without blocking concurrent operations. This is among the most challenging concurrent data structures to implement correctly because resizing requires migrating entries while concurrent operations continue on both the old and new tables.

## Requirements

1. Implement a lock-free hash map using open addressing with Robin Hood hashing: entries are stored in a flat array of slots, each slot containing an atomic key-value pair with metadata (hash, displacement from ideal position); during probing, if the current entry has a shorter probe distance than the entry being inserted, swap them (Robin Hood: steal from the rich, give to the poor).
2. Each slot must be manipulable atomically: use a 128-bit CAS (or split the slot into two 64-bit atomic words) to atomically read and update the key-value pair, or use a state machine approach with per-slot `atomic.Uint64` for the key hash and `atomic.Pointer[V]` for the value.
3. Support `Get(key K) (V, bool)` using a lock-free probe sequence: hash the key, start at the ideal slot, and probe forward until finding the key, an empty slot, or a slot with a shorter displacement (Robin Hood property guarantees the key is not present beyond this point).
4. Support `Put(key K, value V) (V, bool)` that inserts or updates atomically: claim the slot using CAS, handling displacement swaps for Robin Hood optimization.
5. Support `Delete(key K) (V, bool)` using tombstone-based deletion with backward shift deletion for compaction: mark the slot as deleted, then attempt to shift subsequent entries backward to fill the gap, maintaining the Robin Hood invariant.
6. Implement concurrent resizing: when the load factor exceeds a threshold (default 0.75), allocate a new table of double the size and incrementally migrate entries from the old table to the new table, with concurrent operations helping the migration. Each operation checks whether migration is in progress and migrates a few entries before performing its own operation on the new table.
7. Support generic key and value types using Go generics, with keys requiring `comparable` and a hash function.
8. Implement `ForEach(func(key K, value V) bool)` that iterates over all key-value pairs in an arbitrary but consistent order, handling concurrent modifications gracefully (may or may not see entries added during iteration).

## Hints

- Robin Hood hashing reduces the variance of probe sequence lengths, which dramatically helps concurrent operations because the maximum probe distance is bounded (typically O(log n)).
- For atomic slot operations, consider using `atomic.Pointer[entry[K,V]]` per slot, where entries are immutable (copy-on-write); this avoids the need for 128-bit CAS.
- Resizing state machine: the hash map has an atomic `state` field that is `NORMAL`, `GROWING`, or `SHRINKING`. When `GROWING`, a new table is allocated and a `migrateIndex` atomic counter tracks which slots have been migrated.
- During migration, each slot in the old table transitions through states: `NORMAL -> MIGRATING -> MIGRATED`. A concurrent operation on a `MIGRATED` slot redirects to the new table.
- Tombstones in open-addressing cause probe sequence lengthening; backward shift deletion avoids this but is harder to implement concurrently.
- Use the `xxhash` algorithm or `maphash.Hash` for high-quality hashing.
- Study the Cliff Click lock-free hash table (Java `NonBlockingHashMap`) for resize inspiration.

## Success Criteria

1. 16 concurrent goroutines performing random `Get`, `Put`, and `Delete` operations on a hash map of 1 million entries pass `go test -race` with zero data races.
2. Read throughput with 32 concurrent readers (read-heavy workload, 95% reads) exceeds 20 million ops/sec.
3. Write throughput with 8 concurrent writers exceeds 5 million ops/sec.
4. Concurrent resizing from 1 million to 2 million entries completes without blocking any concurrent operation for more than 1 microsecond.
5. Robin Hood hashing keeps the maximum probe distance below 20 even at 70% load factor.
6. Delete operations correctly remove entries and do not leave tombstones that degrade performance over time (backward shift deletion compacts the table).
7. All operations are linearizable: a concurrent history checker validates that the result of every operation is consistent with some sequential ordering.
8. Throughput scales linearly from 1 to 8 cores (at least 6x speedup at 8 cores for a balanced read/write workload).

## Research Resources

- Cliff Click, "A Lock-Free Wait-Free Hash Table that is as Fast and Space-Efficient as a Sequential One" (2007)
- `NonBlockingHashMap` source -- https://github.com/boundary/high-scale-lib
- Pedro Celis, "Robin Hood Hashing" (PhD thesis, 1986)
- "Concurrent Hash Tables: Fast and General(!)" (Maier et al., 2019) -- survey of concurrent hash table techniques
- Go `sync.Map` source code -- https://cs.opensource.google/go/go/+/refs/tags/go1.22.0:src/sync/map.go
- Go `hash/maphash` package for hash computation
- "The Art of Multiprocessor Programming" (Herlihy & Shavit, 2012) -- Chapter 13: Concurrent Hashing
