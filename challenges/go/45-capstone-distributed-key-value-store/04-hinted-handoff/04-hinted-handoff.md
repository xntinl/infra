<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Hinted Handoff

## The Challenge

Implement a hinted handoff mechanism that preserves write availability when replica nodes are temporarily unavailable. When a coordinator cannot reach one of the N replicas for a write, it must store the write as a "hint" on another live node in the cluster, tagging it with the identity of the intended recipient. When the failed node recovers and rejoins the cluster, hints are replayed to it in order, bringing it up to date without requiring a full anti-entropy repair. Your implementation must handle the case where the hint-storing node itself fails before delivery, must impose backpressure when hint storage exceeds a configurable limit, and must guarantee that hints are delivered exactly once even under concurrent node failures and recoveries.

## Requirements

1. When a write's target replica is unreachable, the coordinator selects the next live node on the hash ring (not already a replica for that key) as a temporary hint recipient and writes the data there with a hint header containing the intended node ID, partition ID, and original timestamp.
2. Hints are stored in a dedicated on-disk hint log per intended recipient, separate from the node's own partition data, using an append-only file with CRC32-checksummed entries for corruption detection.
3. When a previously failed node is detected as alive (via the membership protocol or a successful health check), the hint-holding node opens a streaming RPC connection and replays all pending hints in timestamp order, deleting each hint only after receiving an acknowledgment.
4. Implement exactly-once delivery semantics using a hint ID (UUID) and an idempotency check on the recipient: the recipient maintains a set of recently applied hint IDs and rejects duplicates.
5. Impose a configurable maximum hint storage size per intended recipient (default 256 MB); when exceeded, the oldest hints are dropped and a warning is logged, and the stale replica must rely on anti-entropy for full repair.
6. Support configurable hint TTL (default 3 hours); hints older than the TTL are garbage collected regardless of whether they have been delivered, under the assumption that anti-entropy will handle long-term divergence.
7. Handle cascading failures: if the hint-storing node itself fails before delivering hints, the hints are lost; document this limitation and ensure anti-entropy is the fallback repair mechanism.
8. Expose metrics for hints stored, hints delivered, hints expired, hints dropped due to storage limits, and current hint backlog size per intended recipient.

## Hints

- The hint log can be a simple append-only file with entries formatted as `[4-byte length][16-byte hint-id][4-byte target-node-id][key-length][key][value-length][value][8-byte timestamp][4-byte crc32]`.
- Use `os.O_APPEND|os.O_CREATE|os.O_WRONLY` for the hint log to ensure atomic appends.
- On replay, read the hint log sequentially and delete the file after all hints are acknowledged; if replay is interrupted, resume from the last unacknowledged hint.
- The idempotency set on the recipient can be a bounded LRU cache or a bloom filter with a false-positive-safe fallback.
- For testing, simulate node failure by closing the listener socket and node recovery by reopening it.
- Backpressure: when hint storage is near the limit, the coordinator should return a degraded-write warning to the client rather than silently dropping hints.

## Success Criteria

1. With `N=3, W=2` and one replica down, writes continue to succeed and the hint is stored on a substitute node.
2. When the failed replica recovers, all hinted writes are replayed within 10 seconds and the replica's data matches the other two replicas.
3. Replaying 10,000 hints with duplicate hint IDs results in exactly 10,000 unique keys on the recipient (no duplicates applied).
4. Hints exceeding the 256 MB storage limit cause the oldest hints to be dropped, and the metric counter reflects the exact number dropped.
5. Hints older than the TTL are garbage collected and do not appear in the replay stream.
6. The hint delivery protocol tolerates the recipient disconnecting mid-replay and resumes correctly on the next attempt without re-delivering acknowledged hints.
7. All hint metrics are accurate and queryable via a programmatic API.

## Research Resources

- "Dynamo: Amazon's Highly Available Key-Value Store" (DeCandia et al., 2007) -- hinted handoff protocol
- Apache Cassandra hinted handoff documentation -- https://cassandra.apache.org/doc/latest/operating/hints.html
- Riak KV hinted handoff -- https://docs.riak.com/riak/kv/latest/learn/glossary/#hinted-handoff
- Go `os` package for append-only file operations
- Go `hash/crc32` package for entry checksums
- UUID generation in Go: `github.com/google/uuid` or `crypto/rand`
