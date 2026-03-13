# 14. Chaos Testing Framework

<!--
difficulty: insane
concepts: [chaos-engineering, fault-injection, network-partition, latency-injection, process-failure, invariant-checking, jepsen-style-testing]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [raft-leader-election, distributed-locking, sharded-key-value-store]
-->

## Prerequisites

- Go 1.22+ installed
- Completed at least one distributed system implementation (Raft, KV store, or locking)
- Understanding of failure modes in distributed systems

## Learning Objectives

- **Create** a chaos testing framework that injects faults into distributed system simulations
- **Analyze** system behavior under network partitions, node failures, and message delays
- **Evaluate** whether distributed system invariants hold under adversarial conditions

## The Challenge

Distributed systems are designed to handle failures, but how do you verify they actually do? Chaos testing systematically injects faults -- network partitions, node crashes, message delays, clock skew -- and checks that system invariants (linearizability, no data loss, consistency) still hold.

Build a chaos testing framework that can be applied to any of your previous distributed system implementations. The framework should support pluggable fault injection, automated invariant checking, and reproducible test scenarios.

## Requirements

1. Implement a `FaultInjector` with pluggable fault types:
   - Network partition: isolate a set of nodes from the rest
   - Message delay: add latency to messages between specific nodes
   - Message loss: drop a percentage of messages
   - Node crash: stop a node from processing messages
   - Node restart: bring a crashed node back after a delay
   - Clock skew: simulate time drift on specific nodes
2. Implement a `TestRunner` that orchestrates: start cluster, inject faults, run workload, check invariants, report results
3. Implement invariant checkers:
   - Linearizability: all operations appear to happen atomically at some point between invocation and response
   - No data loss: all acknowledged writes are readable after faults heal
   - Consistency: all nodes agree on the final state after recovery
4. Implement a history recorder that logs every operation (invoke, response, fault event) with timestamps for post-hoc analysis
5. Apply the framework to at least one previous exercise (Raft, distributed lock, or KV store) and find or confirm correct behavior under:
   - Leader failure during active writes
   - Network partition splitting the cluster
   - Simultaneous crash of minority nodes
6. Implement reproducible randomized testing: seed the random number generator, record the seed, and allow replaying a specific test run
7. Generate a test report showing: fault timeline, operation outcomes, invariant check results, and any violations found

## Hints

- For linearizability checking, use a sequential specification: record all operations with their invoke and response times, then check if there exists a legal sequential ordering consistent with the real-time order. This is NP-complete in general but tractable for small histories.
- For simulated systems (in-process), implement faults by intercepting the communication layer. Wrap channels with a `FaultyTransport` that can drop, delay, or partition messages.
- A network partition divides nodes into groups that can communicate internally but not across groups. Implement as a connectivity matrix that the transport layer checks.
- Jepsen's approach: run a workload (concurrent reads and writes), inject faults, record history, check linearizability. Use the Knossos or Porcupine library for linearizability checking.
- `github.com/anishathalye/porcupine` is a Go linearizability checker that can verify operation histories.
- Seed-based reproducibility: use `rand.New(rand.NewSource(seed))` for all random decisions. Log the seed at the start of each run.

## Success Criteria

1. The fault injector supports at least 5 fault types
2. Faults can be precisely scheduled (at specific times or after specific events)
3. The invariant checker correctly detects violations (test with a known-buggy implementation)
4. The framework correctly validates a working implementation (e.g., Raft survives minority failures)
5. The history recorder captures a complete, replayable log of operations and faults
6. Randomized tests are reproducible given the same seed
7. The test report clearly shows the fault timeline and invariant results

## Research Resources

- [Jepsen](https://jepsen.io/) -- the gold standard for distributed systems testing
- [Porcupine](https://github.com/anishathalye/porcupine) -- linearizability checker in Go
- [Principles of Chaos Engineering](https://principlesofchaos.org/) -- chaos engineering manifesto
- [Netflix Chaos Monkey](https://netflix.github.io/chaosmonkey/) -- production chaos testing
- [FoundationDB Simulation Testing](https://www.youtube.com/watch?v=4fFDFbi3toc) -- deterministic simulation testing

## What's Next

Continue to [15 - Paxos Consensus](../15-paxos-consensus/15-paxos-consensus.md) to implement the classic consensus algorithm.

## Summary

- Chaos testing systematically verifies distributed system behavior under faults
- Fault injection includes: network partitions, node crashes, message delays/loss, clock skew
- Invariant checking verifies correctness properties: linearizability, no data loss, consistency
- Reproducible random testing uses seeded random generators for replay
- A complete history log enables post-hoc analysis of failures
- Apply chaos testing to every distributed system you build to verify correctness claims
