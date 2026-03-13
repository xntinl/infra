# 8. Raft Snapshots

<!--
difficulty: insane
concepts: [raft, snapshot, log-compaction, installsnapshot-rpc, state-transfer, checkpoint, log-truncation]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [raft-leader-election, raft-log-replication]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Raft leader election and log replication (exercises 06-07)
- Reading of Raft paper Section 7 (Log Compaction)

## Learning Objectives

- **Create** a snapshot mechanism that compacts the Raft log without losing committed state
- **Analyze** the interaction between snapshots and log replication (InstallSnapshot RPC)
- **Evaluate** snapshot policies (when to snapshot, size thresholds, concurrent snapshotting)

## The Challenge

Without log compaction, the Raft log grows unboundedly -- every command ever applied is stored forever. Snapshots solve this by periodically serializing the state machine into a checkpoint and discarding all log entries up to the snapshot point. When a follower is far behind (its needed log entries have been discarded), the leader sends its snapshot via InstallSnapshot RPC.

Extend your Raft implementation with snapshot support. Implement snapshot creation, log truncation, InstallSnapshot RPC, and snapshot-based recovery.

## Requirements

1. Implement snapshot creation: serialize the state machine state and record the last included index/term
2. Implement log truncation: discard all log entries up to and including the snapshot index
3. Implement InstallSnapshot RPC: the leader sends a snapshot to a follower that is too far behind for incremental replication
4. Implement snapshot application on the follower: replace the state machine state and adjust the log
5. Implement a configurable snapshot policy: trigger snapshots when the log exceeds a size threshold (e.g., 1000 entries)
6. Handle the edge case where a snapshot is received while the node has uncommitted entries that conflict with the snapshot
7. Ensure that snapshots are taken concurrently (do not block the main Raft loop during serialization)
8. Write tests for: snapshot creation and log truncation, InstallSnapshot delivery, follower recovery from snapshot, snapshot during active replication

## Hints

- The snapshot includes: last included index, last included term, and the serialized state machine state.
- After snapshotting, keep only log entries after the snapshot index. Adjust all index-based lookups (the log may no longer start at index 0).
- InstallSnapshot is used when `nextIndex[peer]` points to an entry that has been discarded. Instead of AppendEntries, send the snapshot.
- When a follower receives InstallSnapshot: if the snapshot is newer than its current state, replace the state machine and discard conflicting log entries. If the snapshot is older, ignore it.
- For concurrent snapshotting, take a copy of the state machine (or use copy-on-write) and serialize in a background goroutine. Apply the log truncation only after serialization completes.
- Use `encoding/gob` or `encoding/json` for state machine serialization in the simulation.

## Success Criteria

1. Snapshots correctly capture the state machine state at a given log index
2. Log entries before the snapshot are discarded, reducing memory usage
3. InstallSnapshot correctly transfers state to a far-behind follower
4. The follower recovers from a snapshot and resumes normal replication
5. Concurrent snapshotting does not block the main Raft loop
6. All index-based operations work correctly after log truncation
7. No data races (`go test -race`)

## Research Resources

- [Raft paper Section 7: Log Compaction](https://raft.github.io/raft.pdf)
- [etcd/raft snapshot handling](https://github.com/etcd-io/raft/blob/main/log.go)
- [Hashicorp Raft snapshot interface](https://github.com/hashicorp/raft/blob/main/snapshot.go)
- [Students' Guide to Raft: Snapshots](https://thesquareplanet.com/blog/students-guide-to-raft/#an-aside-on-snapshots)

## What's Next

Continue to [09 - CRDTs](../09-crdts/09-crdts.md) to explore conflict-free replicated data types as an alternative to consensus.

## Summary

- Snapshots compact the Raft log by serializing the state machine and discarding old entries
- InstallSnapshot RPC transfers state to followers too far behind for incremental replication
- Log truncation requires adjusting all index-based lookups
- Snapshot policies balance compaction frequency against serialization cost
- Concurrent snapshotting avoids blocking the consensus loop
- Snapshots are essential for long-running Raft clusters to prevent unbounded memory growth
