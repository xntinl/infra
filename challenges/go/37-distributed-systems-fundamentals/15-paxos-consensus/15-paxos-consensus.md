# 15. Paxos Consensus

<!--
difficulty: insane
concepts: [paxos, single-decree-paxos, multi-paxos, proposer, acceptor, learner, prepare-promise, accept-accepted, majority-quorum]
tools: [go]
estimated_time: 4h
bloom_level: create
prerequisites: [raft-leader-election, raft-log-replication, distributed-locking]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Raft exercises (06-08) for context on consensus algorithms
- Read Lamport's "Paxos Made Simple" paper

## Learning Objectives

- **Create** a Single-Decree Paxos implementation and extend it to Multi-Paxos
- **Analyze** the differences between Paxos and Raft in design philosophy and implementation complexity
- **Evaluate** liveness and safety properties of the Paxos protocol

## The Challenge

Paxos is the foundational consensus algorithm. It predates Raft and is more general but harder to understand and implement. Single-Decree Paxos agrees on a single value through two phases: Prepare/Promise and Accept/Accepted. Multi-Paxos extends this to a sequence of values (a replicated log) by electing a distinguished proposer that can skip the Prepare phase for subsequent slots.

Implement Single-Decree Paxos first, verify it with correctness tests, then extend to Multi-Paxos for replicated log consensus. Compare the implementation complexity and performance with your Raft implementation.

## Requirements

1. Implement Single-Decree Paxos with three roles: Proposer, Acceptor, Learner
2. Implement Phase 1 (Prepare/Promise): the proposer sends Prepare(n) to acceptors; each acceptor responds with Promise(n, accepted_value) if n is the highest proposal it has seen
3. Implement Phase 2 (Accept/Accepted): if the proposer receives promises from a majority, it sends Accept(n, v); acceptors accept if they have not promised a higher proposal
4. Implement the learner: learns the chosen value when a majority of acceptors have accepted the same proposal
5. Handle dueling proposers: two proposers competing with increasing proposal numbers
6. Extend to Multi-Paxos: agree on a sequence of values (one Paxos instance per log slot)
7. Implement the Multi-Paxos leader optimization: the distinguished proposer skips Phase 1 for consecutive slots
8. Compare with Raft: document the structural differences and implementation complexity

## Hints

- Proposal numbers must be unique and ordered. Use `(round, proposerID)` pairs with lexicographic ordering.
- An acceptor tracks: highest promised proposal number, highest accepted proposal number and value.
- If a proposer receives a Promise with an already-accepted value, it must propose that value (not its own). This is the key safety mechanism.
- Single-Decree Paxos guarantees safety (no two nodes learn different values) but not liveness (dueling proposers can livelock). In practice, use randomized backoff or a leader.
- Multi-Paxos uses one Paxos instance per log index. The leader pre-empts Phase 1 by maintaining a stable proposal number, going directly to Phase 2 for each new slot.
- Use channels to simulate message passing. Add random delays to expose timing-dependent bugs.
- Implement a `PaxosAcceptor` that handles both Prepare and Accept messages for multiple slots.

## Success Criteria

1. Single-Decree Paxos correctly agrees on a value with a single proposer
2. With competing proposers, the protocol eventually converges (no conflicting values)
3. Safety holds: no two learners learn different values for the same slot
4. Multi-Paxos correctly replicates a sequence of values
5. The leader optimization skips Phase 1 for consecutive slots
6. Tests demonstrate correct behavior under message delays and proposer failures
7. The comparison with Raft clearly documents structural differences

## Research Resources

- [Paxos Made Simple (Lamport)](https://lamport.azurewebsites.net/pubs/paxos-simple.pdf) -- the clearest Paxos explanation
- [The Part-Time Parliament (Lamport)](https://lamport.azurewebsites.net/pubs/lamport-paxos.pdf) -- the original Paxos paper
- [Paxos Made Live (Chandra et al.)](https://research.google/pubs/pub33002/) -- lessons from Google's production Paxos
- [Paxos vs Raft](https://www.youtube.com/watch?v=JEpsBg0AO6o) -- comparison talk
- [Understanding Paxos (blog)](https://understandingpaxos.wordpress.com/) -- step-by-step walkthrough

## What's Next

Continue to [16 - Two-Phase Commit](../16-two-phase-commit/16-two-phase-commit.md) to implement distributed transaction coordination.

## Summary

- Paxos is the foundational consensus algorithm using Prepare/Promise and Accept/Accepted phases
- Safety is guaranteed: no two nodes can learn different values for the same decree
- Liveness requires a leader or randomized backoff to prevent dueling proposers
- Multi-Paxos extends Single-Decree to replicated logs with a leader optimization
- Paxos is more general than Raft but harder to implement correctly
- Understanding Paxos provides deep insight into the theory of distributed consensus
