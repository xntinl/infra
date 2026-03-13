<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Message Retention and Log Compaction

A message queue that keeps all messages forever will eventually exhaust disk space. But simply deleting old messages can destroy valuable state. Your task is to implement two complementary data lifecycle strategies: time-based and size-based retention (delete messages older than X or when the log exceeds Y bytes) and log compaction (retain only the latest message per key, like a changelog). Log compaction is the feature that transforms a message queue from a temporary buffer into a durable, queryable state store -- it is what enables Kafka's changelog topics and compacted topics. This is one of the most technically challenging components in a message queue.

## Requirements

1. Implement time-based retention: configure a `retention.ms` per topic (default 7 days). A background goroutine periodically scans the topic's segments and deletes any segment whose newest message timestamp is older than `now - retention.ms`. Only delete entire segments (not individual messages within a segment) for efficiency. Implement `RetentionPolicy` as an interface with `ShouldDelete(segment *Segment) bool` so custom policies can be plugged in.

2. Implement size-based retention: configure a `retention.bytes` per topic (default unlimited). When the total size of all segments exceeds this limit, delete the oldest segments until the total size is within the limit. Size-based retention works independently of time-based retention; both can be active simultaneously (whichever triggers first wins). Implement `MaxSegmentBytes` to control individual segment size.

3. Implement log compaction: for topics with `cleanup.policy = compact`, maintain only the latest value for each unique key. The compaction process reads dirty (uncompacted) segments, builds a key-to-latest-offset map, then writes a new clean segment containing only the records with the latest offset for each key, in their original offset order. Deleted keys are represented by tombstones (messages with a null value) which are retained for a configurable `delete.retention.ms` before being removed entirely.

4. Implement the compaction algorithm with minimal impact on active producers and consumers. The compactor must: identify segments eligible for compaction (all segments before the active segment), read messages from eligible segments, build an offset map (`map[key]latestOffset`), write a new compacted segment containing only the latest-offset messages, atomically swap the compacted segment for the original segments (rename + delete), and update any active consumers' positions if their current offset was in a compacted-away section. The compaction must never lose a message that a consumer has not yet read.

5. Implement the "dirty ratio" trigger for compaction: track the ratio of dirty (potentially superseded) log bytes to total log bytes. When the dirty ratio exceeds a configurable threshold (`min.cleanable.dirty.ratio`, default 0.5), trigger compaction. Also support time-based compaction triggers (`compaction.interval`, default 15 minutes). The compaction runs in a background goroutine and must not block producers or consumers.

6. Implement tombstone handling: a message with a non-null key and null value is a tombstone indicating the key has been deleted. During compaction, tombstones are retained for `delete.retention.ms` (default 24 hours) so that downstream consumers and replicas can observe the deletion. After the retention period, tombstones are removed entirely during the next compaction cycle. Track tombstone creation time in a per-key metadata store.

7. Implement a hybrid policy (`cleanup.policy = compact,delete`): combine compaction and time-based retention. Messages are compacted (keeping latest per key) AND segments older than `retention.ms` are deleted. This ensures both that the log does not grow unboundedly and that the latest state per key is retained. Handle the edge case where the latest value for a key is in a segment that would be deleted by time-based retention (the compacted value must be preserved).

8. Write tests covering: time-based retention deleting old segments (mock time to avoid waiting), size-based retention deleting segments when size exceeds limit, log compaction correctly retaining only the latest value per key (publish 1000 messages with 100 keys, compact, verify only 100 messages remain with latest values), tombstone retention and eventual removal, compaction not affecting active consumers (consumer reading during compaction still receives all messages), concurrent compaction and production (no data races, no lost messages), dirty ratio calculation and trigger, hybrid policy behavior, and a benchmark measuring compaction throughput (MB/s processed) and space savings ratio.

## Hints

- Compaction is fundamentally a merge-sort-like operation: read all records from dirty segments, keep only the ones with the latest offset per key, and write them out. The key insight is that you only need to keep the offset map in memory (not all messages), then do a second pass to write the compacted output.
- For the offset map, iterate through segments from newest to oldest. The first time you see a key, record its offset. Skip all subsequent occurrences. This builds the "keep set."
- Atomic segment swap: write the compacted segment to a temporary file (e.g., `.compacting` suffix), then rename it to replace the original. Renames are atomic on POSIX filesystems. Delete the original segments only after the new one is confirmed.
- The "high watermark" concept is important: never compact messages at or above the high watermark (the latest offset that all consumers have read up to). This prevents compacting away messages that active consumers haven't seen.
- For the dirty ratio, maintain running counters: total log bytes and bytes in segments newer than the last compacted offset. The ratio is `dirty_bytes / total_bytes`.
- Tombstones need metadata: store `tombstoneCreatedAt time.Time` either in the message headers or in a separate metadata file per segment.

## Success Criteria

1. Time-based retention correctly deletes segments older than the configured retention period, freeing disk space.
2. Size-based retention keeps total log size within the configured limit by deleting oldest segments.
3. Log compaction reduces 10,000 messages with 100 unique keys down to exactly 100 messages (one per key), each with the latest value.
4. Tombstones are retained during compaction for the configured period and removed after expiration.
5. Active consumers are not affected by compaction: a consumer reading from a compacting topic receives all messages without gaps.
6. Concurrent compaction and production produce no data races (verified with `-race`) and no lost messages.
7. The hybrid policy correctly combines compaction and time-based retention, preserving latest values even for keys whose original messages would be time-expired.
8. Compaction achieves at least 100 MB/s processing throughput and reduces log size by at least 70% when the average key has 10+ versions.

## Research Resources

- [Kafka Log Compaction](https://kafka.apache.org/documentation/#compaction)
- [Log Compaction Design (Confluent)](https://developer.confluent.io/courses/architecture/compaction/)
- [How Kafka's Log Compaction Works Under the Hood](https://www.confluent.io/blog/log-compaction-theory-and-practice/)
- [Change Data Capture with Compacted Topics](https://debezium.io/documentation/reference/stable/tutorial.html)
- [Designing Data-Intensive Applications - Log Compaction](https://dataintensive.net/)
- [LSM-Tree Compaction Strategies (for comparison)](https://github.com/facebook/rocksdb/wiki/Compaction)
