# 39. MapReduce Framework

```yaml
difficulty: insane
languages: [go]
time_estimate: 30-45 hours
tags: [distributed-systems, mapreduce, fault-tolerance, parallel-processing, distributed-sort]
bloom_level: [evaluate, create]
```

## Prerequisites

- Distributed systems: coordinator-worker architecture, RPC, task scheduling
- Concurrency: goroutines, channels, mutexes, `context` with deadlines
- File I/O: reading, partitioning, and writing large files efficiently
- Serialization: encoding intermediate key-value pairs for network transfer or disk storage
- Hash partitioning: distributing keys across reduce tasks uniformly

## Learning Objectives

After completing this challenge you will be able to:

- **Design** a coordinator-worker framework that distributes computation across multiple processes
- **Implement** fault tolerance through task re-execution and idempotent output
- **Evaluate** the performance impact of combiners, speculative execution, and partition strategies
- **Build** a shuffle phase that partitions, sorts, and groups intermediate data for reducers
- **Create** real distributed applications (word count, inverted index, distributed sort) on your framework

## The Challenge

Build a MapReduce framework inspired by the original Google paper. A coordinator process assigns map and reduce tasks to worker processes. Workers execute user-defined map and reduce functions on data splits. The framework handles intermediate data partitioning, shuffling, sorting, fault tolerance (re-executing failed tasks), and speculative execution for stragglers.

Use the local filesystem to simulate a distributed file system: different directories represent different "nodes." Workers read input splits and write intermediate/output files to these directories.

## Requirements

1. **Coordinator**: Manages the job lifecycle. Splits input files into M map tasks and R reduce tasks (configurable). Tracks task state (idle, in-progress, completed). Assigns tasks to workers via RPC. Detects worker failure by timeout and re-assigns their tasks.

2. **Worker**: Registers with the coordinator and requests tasks. Executes map or reduce functions provided as plugins (Go plugin system or function values). Reports completion back to the coordinator. A single worker binary handles both map and reduce tasks.

3. **Map phase**: Each map task reads one input split, applies the user-defined map function to each record, and produces intermediate key-value pairs. Intermediate output is partitioned into R files using `hash(key) % R` for the shuffle.

4. **Shuffle and sort**: Between map and reduce phases, each reduce task reads its partition from all map outputs, merges them, and sorts by key. All values for the same key are grouped together before calling the reduce function.

5. **Reduce phase**: Each reduce task receives a sorted stream of `(key, []values)` groups. The user-defined reduce function processes each group and produces output key-value pairs. Output is written to a final output file per reduce task.

6. **Combiner**: An optional combiner function runs on the map side after producing intermediate output, performing local aggregation before the shuffle. For associative and commutative reduce functions (like sum or count), this reduces network/disk transfer.

7. **Fault tolerance**: If a worker fails (detected by heartbeat timeout), all its in-progress tasks are reset to idle and re-assigned. Completed map tasks from a failed worker must also be re-executed because their intermediate output is on the failed worker's local disk. Completed reduce tasks are not re-executed (output is in the global filesystem).

8. **Speculative execution**: If a map or reduce task takes significantly longer than the average completion time for its phase (e.g., 1.5x the median), launch a backup execution on another worker. Use the result from whichever copy finishes first.

9. **Example applications**: Implement three applications on your framework:
   - **Word count**: Count occurrences of each word across all input files
   - **Inverted index**: For each word, produce a list of files containing it
   - **Distributed sort**: Sort all records across input files, producing globally sorted output

10. **Monitoring**: The coordinator exposes a simple HTTP status page showing: active workers, task states, phase progress, and elapsed time.

## Hints

1. Design the coordinator as a state machine: IDLE -> MAP_PHASE -> SHUFFLE -> REDUCE_PHASE -> COMPLETE. Each phase transition happens only when all tasks in the current phase are complete. This simplifies reasoning about correctness.

2. For fault tolerance, make map output atomic: write to a temporary file, then rename to the final path. This ensures that partial output from a crashed worker is never read by a reducer. The same applies to reduce output.

## Acceptance Criteria

- [ ] Coordinator correctly splits input into M map tasks and assigns them to workers
- [ ] Map phase produces partitioned intermediate files (R partitions per map task)
- [ ] Shuffle phase collects and sorts intermediate data per reduce partition
- [ ] Reduce phase produces correct output files
- [ ] Combiner reduces intermediate data volume for associative operations
- [ ] Worker failure during map triggers re-execution of that worker's tasks
- [ ] Worker failure during reduce triggers re-execution of only in-progress reduce tasks
- [ ] Speculative execution launches backup tasks for stragglers
- [ ] Word count produces correct word frequencies across multiple input files
- [ ] Inverted index produces correct file lists per word
- [ ] Distributed sort produces globally sorted output across all reduce partitions
- [ ] End-to-end test: 4 workers, kill one during map phase, job still completes correctly

## Resources

- [Dean & Ghemawat: "MapReduce: Simplified Data Processing on Large Clusters" (2004)](https://research.google/pubs/pub62/) - The original MapReduce paper
- [MIT 6.824: Distributed Systems Lab 1 - MapReduce](https://pdos.csail.mit.edu/6.824/labs/lab-mr.html) - The classic MapReduce lab assignment
- [Apache Hadoop MapReduce source](https://github.com/apache/hadoop/tree/trunk/hadoop-mapreduce-project) - Production implementation for reference
- [Go RPC package](https://pkg.go.dev/net/rpc) - Go's built-in RPC library
- [Go plugin package](https://pkg.go.dev/plugin) - Dynamic loading of map/reduce functions
- [Designing Data-Intensive Applications, Chapter 10](https://dataintensive.net/) - Batch processing patterns and MapReduce
