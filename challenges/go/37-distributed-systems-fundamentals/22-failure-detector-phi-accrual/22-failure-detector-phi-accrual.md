# 22. Failure Detector: Phi Accrual

<!--
difficulty: insane
concepts: [failure-detection, phi-accrual, heartbeat, suspicion-level, adaptive-threshold, exponential-distribution, fd-quality-metrics]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [gossip-protocol, leader-election-bully-algorithm]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of gossip protocols and cluster membership
- Basic statistics knowledge (mean, variance, distributions)

## Learning Objectives

- **Create** a Phi Accrual failure detector that outputs a continuous suspicion level instead of a binary alive/dead decision
- **Analyze** the relationship between heartbeat intervals, network jitter, and detection accuracy
- **Evaluate** the Phi Accrual detector against fixed-timeout detectors for false positive rate and detection time

## The Challenge

Traditional failure detectors use fixed timeouts: if a heartbeat is not received within T seconds, the node is declared dead. This is brittle -- too short and you get false positives during network jitter; too long and you have slow detection. The Phi Accrual failure detector instead outputs a continuous suspicion level (phi) based on the statistical distribution of heartbeat arrival times. A higher phi means higher confidence that the node has failed. The application chooses its own threshold.

Implement the Phi Accrual failure detector used by Cassandra and Akka. Track heartbeat inter-arrival times, model their distribution, compute the phi value for the current time, and let the application set a threshold for declaring failure.

## Requirements

1. Implement a `HeartbeatHistory` that tracks the inter-arrival times of heartbeats from a specific peer using a sliding window
2. Compute the mean and variance of inter-arrival times from the history
3. Implement the phi calculation: `phi = -log10(1 - F(timeSinceLastHeartbeat))` where F is the cumulative distribution function of the normal distribution with the observed mean and variance
4. Implement a `PhiAccrualDetector` that manages multiple peers:
   - `Heartbeat(peerID string)` -- record a heartbeat from a peer
   - `Phi(peerID string) float64` -- compute the current phi value for a peer
   - `IsAvailable(peerID string, threshold float64) bool` -- check if phi is below the threshold
5. Implement an adaptive threshold: automatically adjust the threshold based on observed false positive rate
6. Compare with a fixed-timeout detector: run both under the same workload with variable network latency and compare false positive rates and detection times
7. Implement quality metrics: completeness (percentage of actual failures detected), accuracy (percentage of detections that are correct), and detection time
8. Write a simulation that introduces variable heartbeat delays, node failures, and network jitter, and measures detector performance

## Hints

- The normal distribution CDF can be approximated with `0.5 * (1 + math.Erf((x - mean) / (math.Sqrt(2) * stddev)))`.
- Use a sliding window (e.g., last 1000 inter-arrival times) to adapt to changing network conditions.
- Typical phi thresholds: 8 (moderate confidence, faster detection) to 16 (high confidence, slower detection). Cassandra uses 8.
- Bootstrap the detector with an initial heartbeat interval estimate (e.g., the expected interval from configuration).
- Phi increases monotonically the longer you wait since the last heartbeat. Very small phi (<1) means the node is almost certainly alive. Phi > 8 means high suspicion.
- The fixed-timeout detector is a special case: it is equivalent to phi with a step function instead of a smooth curve.
- Add random jitter to simulated heartbeats: `interval + rand.NormFloat64() * jitter`.

## Success Criteria

1. The phi value increases smoothly as time since last heartbeat increases
2. The detector correctly identifies failed nodes with high phi values
3. The detector does not false-positive on live nodes experiencing normal jitter
4. The adaptive threshold adjusts based on observed network conditions
5. Quality metrics show the phi detector outperforms fixed-timeout on false positive rate
6. The simulation demonstrates detection under varying network conditions
7. The sliding window adapts to changes in heartbeat interval distribution

## Research Resources

- [The Phi Accrual Failure Detector (Hayashibara et al.)](https://www.researchgate.net/publication/29682135_The_ph_accrual_failure_detector) -- the original paper
- [Cassandra Failure Detection](https://cassandra.apache.org/doc/latest/cassandra/architecture/dynamo.html#failure-detection) -- production use
- [Akka Phi Accrual Failure Detector](https://doc.akka.io/docs/akka/current/typed/failure-detector.html) -- another production implementation
- [Unreliable Failure Detectors for Reliable Distributed Systems (Chandra & Toueg)](https://dl.acm.org/doi/10.1145/226643.226647) -- foundational theory

## What's Next

Continue to [23 - Quorum-Based Replication](../23-quorum-based-replication/23-quorum-based-replication.md) to implement quorum protocols for tunable consistency.

## Summary

- The Phi Accrual detector outputs a continuous suspicion level instead of binary alive/dead
- Phi is computed from the statistical distribution of heartbeat inter-arrival times
- Higher phi means higher confidence that the node has failed
- The application chooses the threshold, balancing detection speed vs false positive rate
- A sliding window of inter-arrival times adapts to changing network conditions
- Phi Accrual outperforms fixed-timeout detectors under variable network conditions
- Used in production by Cassandra, Akka, and other distributed systems
