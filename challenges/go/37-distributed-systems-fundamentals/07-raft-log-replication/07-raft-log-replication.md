# 7. Raft Log Replication

<!--
difficulty: insane
concepts: [raft, log-replication, commit-index, append-entries, log-matching, safety-property, state-machine]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [raft-leader-election]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Raft leader election (exercise 06)
- Thorough reading of Raft paper Sections 5.3 and 5.4

## Learning Objectives

- **Create** the log replication component of Raft, extending the leader election from exercise 06
- **Analyze** how Raft maintains log consistency across replicas through the AppendEntries mechanism
- **Evaluate** correctness under leader changes, log conflicts, and concurrent client requests

## The Challenge

Log replication is the core of Raft. The leader receives client commands, appends them to its log, replicates them to followers via AppendEntries RPCs, and commits entries once a majority of nodes have them. The Log Matching Property guarantees that if two logs contain an entry with the same index and term, all preceding entries are identical.

Extend your leader election implementation to support log replication. Handle log conflicts (when a new leader has a different log than followers), implement the commit mechanism, and apply committed entries to a state machine.

## Requirements

1. Implement the `LogEntry` struct and log management:
   - Each entry has a term, index, and command
   - The leader appends client commands to its log
   - AppendEntries RPC sends log entries with `prevLogIndex` and `prevLogTerm` for consistency checking
2. Implement the Log Matching Property:
   - If a follower's log does not match at `prevLogIndex`/`prevLogTerm`, reject the AppendEntries and let the leader decrement `nextIndex` and retry
   - Conflicting entries are overwritten by the leader's entries
3. Implement the commit mechanism:
   - The leader tracks `matchIndex` for each follower
   - An entry is committed when stored on a majority of nodes
   - The leader advances `commitIndex` and applies committed entries to the state machine
   - Followers learn the `commitIndex` from the leader's AppendEntries messages
4. Implement a simple state machine (key-value store) that applies committed commands
5. Handle leader changes: a new leader may need to replicate entries from its own log that the old leader started but did not commit
6. Implement client request handling: accept commands, replicate, wait for commitment, return result
7. Write tests for: basic replication, leader failure mid-replication, log conflict resolution, client request linearizability
8. Ensure the Election Safety property: at most one leader per term, and a new leader's log contains all committed entries from previous terms

## Hints

- The leader maintains `nextIndex[peer]` (next entry to send) and `matchIndex[peer]` (highest replicated entry) for each follower.
- On AppendEntries rejection, decrement `nextIndex` and retry. For faster convergence, the follower can include the conflicting term and the first index of that term in its rejection response.
- Commitment rule: the leader only commits entries from its current term (not previous terms). Previous-term entries are committed indirectly when a current-term entry at a higher index is committed.
- Apply entries to the state machine in order. Track `lastApplied` to avoid double-applying.
- Use a separate "applier" goroutine that watches `commitIndex` and applies entries between `lastApplied` and `commitIndex`.
- Client requests should block until the entry is committed or a timeout occurs. If the leader changes, the client must retry with the new leader.

## Success Criteria

1. Client commands are replicated to all followers and committed by majority
2. The state machine produces consistent results across all nodes
3. Log conflicts from previous terms are resolved correctly
4. A new leader successfully replicates entries from its log
5. The commitment rule prevents unsafe commits from previous terms
6. All committed entries survive leader changes
7. No data races (`go test -race`)

## Research Resources

- [In Search of an Understandable Consensus Algorithm (Ongaro & Ousterhout)](https://raft.github.io/raft.pdf) -- Sections 5.3 and 5.4
- [Students' Guide to Raft](https://thesquareplanet.com/blog/students-guide-to-raft/) -- common log replication mistakes
- [Raft Scope visualization](https://raft.github.io/#raftscope) -- visualize log replication
- [etcd/raft log implementation](https://github.com/etcd-io/raft/blob/main/log.go) -- production reference

## What's Next

Continue to [08 - Raft Snapshots](../08-raft-snapshots/08-raft-snapshots.md) to add log compaction through snapshots.

## Summary

- Raft log replication ensures all nodes apply the same commands in the same order
- The Log Matching Property guarantees consistency through `prevLogIndex`/`prevLogTerm` checks
- Entries are committed when stored on a majority of nodes
- The leader only commits entries from its current term to prevent unsafe commits
- Log conflicts are resolved by the leader overwriting inconsistent follower entries
- A state machine applies committed entries to produce deterministic results
