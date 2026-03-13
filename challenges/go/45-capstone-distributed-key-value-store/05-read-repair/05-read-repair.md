<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Read Repair

## The Challenge

Build a comprehensive read repair system that detects and fixes inconsistencies between replicas during normal read operations. When a coordinator performs a quorum read and receives responses from multiple replicas, it must compare the returned values and their vector clocks to identify stale replicas, then asynchronously push the most recent version to any replica that returned outdated data. Your implementation must handle the nuances of concurrent read repairs (two coordinators repairing the same key simultaneously), partial failures during repair (a repair write fails because the target is now unreachable), and the interaction between read repair and anti-entropy to avoid redundant work. You must also implement probabilistic read repair where only a configurable percentage of reads trigger the full comparison to reduce overhead on hot keys.

## Requirements

1. On every quorum read (`R >= 2`), the coordinator collects responses from all `N` replicas (not just the `R` required for quorum) using a parallel fan-out with a configurable timeout, comparing values and vector clocks from all respondents.
2. If any replica returned a stale value (its vector clock is dominated by another replica's), initiate an asynchronous background repair that sends the newest value to all stale replicas using the same `Put` RPC used for normal writes.
3. Implement probabilistic read repair with a configurable probability (default 10%): only trigger the full N-way comparison on a random subset of reads to reduce overhead, while still guaranteeing that a read with `R` responses returns the correct value to the client.
4. Handle concurrent read repairs on the same key by making repair writes idempotent: a repair write carries the vector clock of the value being written, and the recipient only applies it if it dominates the recipient's current vector clock for that key.
5. Track read repair statistics per partition: number of reads triggering repair, number of stale replicas detected, number of repair writes issued, number of repair writes that succeeded, and number that failed due to unreachable replicas.
6. Implement a "digest read" optimization: instead of requesting the full value from all N replicas, request the full value from one replica and only a digest (hash of value + vector clock) from the remaining N-1 replicas; only fetch the full value from a replica if its digest differs.
7. Handle the case where read repair conflicts with a concurrent client write: if a repair write arrives at a replica that has since received a newer write from a client, the repair write must be rejected (its vector clock is dominated), and no data is overwritten.
8. Integrate with the anti-entropy system: if read repair detects divergence on more than a configurable threshold of keys (e.g., 1% of recent reads), trigger an early anti-entropy round for the affected partition.

## Hints

- The coordinator already has the vector clock comparison logic from the replication exercise; read repair is an extension that acts on the comparison result.
- For digest reads, hash the concatenation of the serialized value and vector clock using SHA-256; this fits in a single RPC response of 32 bytes.
- Use `math/rand` to implement probabilistic read repair: `if rand.Float64() < repairProbability { triggerFullComparison() }`.
- Repair writes should be fire-and-forget from the coordinator's perspective but tracked via metrics; use a buffered channel as a repair queue with a pool of repair workers.
- To test concurrent read repairs, use `sync.WaitGroup` to synchronize two goroutines performing reads on the same key with an artificially stale replica.
- The early anti-entropy trigger should use a sliding window counter (e.g., last 1,000 reads) to calculate the divergence ratio.

## Success Criteria

1. After artificially making one replica stale for a key, a single quorum read triggers a repair that brings the stale replica up to date within 1 second.
2. Probabilistic read repair at 10% repairs a stale key within approximately 10 reads on average (statistical test over 1,000 trials).
3. Digest reads reduce network bandwidth by at least 80% compared to full-value reads when all replicas are consistent.
4. A concurrent client write during read repair is not overwritten: the client's newer value survives.
5. Two concurrent read repairs on the same stale key do not cause duplicate writes or vector clock anomalies.
6. Read repair statistics accurately reflect the number of stale replicas detected and repairs issued.
7. When the divergence ratio exceeds 1% over a sliding window, an early anti-entropy round is triggered within 5 seconds.

## Research Resources

- "Dynamo: Amazon's Highly Available Key-Value Store" (DeCandia et al., 2007) -- read repair as anti-entropy complement
- Apache Cassandra read repair documentation -- https://cassandra.apache.org/doc/latest/operating/read_repair.html
- Riak read repair -- https://docs.riak.com/riak/kv/latest/learn/concepts/replication/#read-repair
- "Optimistic Replication" (Saito & Shapiro, 2005) -- conflict detection and resolution
- Go `crypto/sha256` for digest computation
- Go `math/rand` for probabilistic triggering
