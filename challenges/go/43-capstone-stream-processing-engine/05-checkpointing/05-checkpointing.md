# 5. Checkpointing

<!--
difficulty: insane
concepts: [checkpointing, distributed-snapshots, chandy-lamport, exactly-once, barrier-alignment, state-backend, snapshot-isolation, recovery]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [43-capstone-stream-processing-engine/04-watermarks-late-data, 19-io-and-filesystem, 15-sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-04 (sources through watermarks) or equivalent stream processing pipeline experience
- Understanding of distributed snapshot algorithms and exactly-once processing semantics
- Completed Section 19 (I/O and filesystem) for state persistence

## Learning Objectives

- **Design** a checkpointing system based on the Chandy-Lamport distributed snapshot algorithm that captures consistent pipeline state without stopping processing
- **Create** a barrier-based checkpoint coordinator that injects checkpoint barriers into the stream, aligns them across parallel channels, and triggers operator state snapshots
- **Evaluate** the trade-offs between checkpoint interval, checkpoint duration, recovery time, and processing latency in a fault-tolerant stream processing engine

## The Challenge

Stream processing engines run continuously for days, weeks, or months. When a failure occurs -- an operator crashes, a machine restarts, or a network partition heals -- the engine must recover without losing or duplicating records. Checkpointing is the mechanism that makes this possible: periodically, the engine captures a consistent snapshot of all operator state and source offsets so that processing can resume from the snapshot point after a failure.

The Chandy-Lamport algorithm provides the theoretical foundation. The coordinator injects special barrier messages into the stream at the sources. As each barrier flows through the pipeline, every operator it reaches snapshots its current state (window contents, aggregation accumulators, watermark positions) and forwards the barrier downstream. When an operator with multiple inputs receives a barrier on one input, it must align barriers: it continues processing records from the barrier'd input's buffer but blocks processing from non-barrier'd inputs until all inputs have received the barrier. Once all operators have reported their snapshots, the checkpoint is complete and durable.

You will implement the full checkpointing lifecycle: barrier injection, barrier alignment for operators with fan-in, operator state serialization to a configurable state backend (local filesystem or pluggable), checkpoint completion tracking, and recovery from the latest completed checkpoint including source offset restoration and operator state rehydration.

## Requirements

1. Define a `CheckpointBarrier` message type containing: checkpoint ID, checkpoint timestamp, and source metadata, distinct from regular `Record` messages in the stream
2. Implement a `CheckpointCoordinator` that triggers checkpoints at a configurable interval by injecting barriers into all source operators
3. Implement barrier forwarding: when an operator receives a barrier, it snapshots its state and forwards the barrier to its output channel
4. Implement barrier alignment for operators with multiple input channels: buffer records from channels that have delivered their barrier while waiting for barriers from remaining channels
5. Define a `StateBackend` interface with `SaveState(checkpointID uint64, operatorID string, state []byte) error` and `LoadState(checkpointID uint64, operatorID string) ([]byte, error)` methods
6. Implement a `FileStateBackend` that writes operator state snapshots to the local filesystem using atomic file writes (write to temp, then rename)
7. Implement operator state serialization: each stateful operator (window operator, aggregation operator) must implement a `Snapshot() ([]byte, error)` and `Restore([]byte) error` method
8. Implement checkpoint completion tracking: the coordinator tracks barrier acknowledgments from all operators and marks a checkpoint as complete when all operators have reported
9. Implement checkpoint cleanup: retain only the last N completed checkpoints and delete older state snapshots
10. Implement recovery: on startup, detect the latest completed checkpoint, restore source offsets, restore operator state via `Restore()`, and resume processing from the checkpoint position
11. Write integration tests that verify: checkpoint creation, recovery after simulated failure, and exactly-once semantics (no duplicate records in output after recovery)

## Hints

- Use a tagged union (interface with marker method) or a separate channel for barriers to distinguish them from regular records -- embedding a `BarrierOrRecord` wrapper avoids needing a separate channel
- For barrier alignment, use a per-input-channel buffer (slice of records) that accumulates records arriving after the barrier from that channel but before barriers from other channels
- For atomic file writes, use `os.CreateTemp` in the target directory followed by `os.Rename` -- rename is atomic on POSIX filesystems
- Serialize operator state using `encoding/gob` or `encoding/json` -- gob is more efficient for Go-native types
- For source offset restoration, sources must implement `Seek(offset int64) error` or equivalent to reposition to the checkpoint offset
- Track checkpoint state in the coordinator using a map of `checkpointID -> map[operatorID]bool` and mark complete when all operators are true
- For exactly-once testing, use a deterministic source (e.g., a sequence of integers), process through a counting window, checkpoint mid-stream, simulate failure, recover, and verify the final count matches the expected value

## Success Criteria

1. Checkpoint barriers flow through the entire pipeline from sources to sinks
2. Barrier alignment correctly buffers records from faster channels while waiting for slower channels
3. Operator state snapshots are written atomically to the state backend
4. Checkpoint completion is correctly tracked across all operators
5. Old checkpoints are cleaned up, retaining only the configured number of recent checkpoints
6. Recovery restores operator state and source offsets from the latest completed checkpoint
7. Processing produces exactly-once results after recovery -- no records are lost or duplicated
8. Checkpointing does not block processing (barriers flow through the pipeline concurrently with records)
9. All tests pass with the `-race` flag enabled

## Research Resources

- [Chandy-Lamport algorithm](https://en.wikipedia.org/wiki/Chandy%E2%80%93Lamport_algorithm) -- the foundational distributed snapshot algorithm
- [Apache Flink checkpointing](https://nightlies.apache.org/flink/flink-docs-stable/docs/dev/datastream/fault-tolerance/checkpointing/) -- production checkpointing implementation reference
- [Lightweight Asynchronous Snapshots (Flink paper)](https://arxiv.org/abs/1506.08603) -- the paper describing Flink's adaptation of Chandy-Lamport
- [Apache Flink state backends](https://nightlies.apache.org/flink/flink-docs-stable/docs/ops/state/state_backends/) -- pluggable state backend design
- [Go encoding/gob package](https://pkg.go.dev/encoding/gob) -- binary serialization for operator state snapshots

## What's Next

Continue to [Parallel Execution](../06-parallel-execution/06-parallel-execution.md) where you will implement data parallelism by partitioning streams across multiple operator instances.

## Summary

- Checkpointing captures consistent pipeline state without stopping processing using the Chandy-Lamport barrier algorithm
- Barrier alignment ensures consistent snapshots across operators with multiple inputs by buffering records between barriers
- Atomic file writes prevent corrupted state snapshots from partial writes during failures
- Checkpoint completion tracking requires acknowledgment from every operator before marking a checkpoint as durable
- Recovery restores both operator state and source offsets to resume processing exactly where it left off
- Exactly-once semantics are achieved by combining checkpointing with source rewind and idempotent state restoration
