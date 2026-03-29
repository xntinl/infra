# 107. SWIM Failure Detector

<!--
difficulty: advanced
category: distributed-systems-extended
languages: [go]
concepts: [swim-protocol, failure-detection, indirect-probing, suspicion-mechanism, membership-protocol]
estimated_time: 8-10 hours
bloom_level: evaluate
prerequisites: [go-basics, goroutines, channels, udp-networking, timer-management, gossip-concepts]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Goroutines, channels, and `select` for concurrent protocol operations
- `net.UDPConn` for datagram communication
- `context.Context` for timeout-bounded operations
- Understanding of gossip protocols and epidemic dissemination (Challenge 83 recommended)
- Timer management: `time.After`, `time.Timer` for protocol timeouts
- Familiarity with failure detection requirements: completeness (all failures detected) and accuracy (no false positives)

## Learning Objectives

- **Evaluate** the completeness and accuracy trade-offs of SWIM failure detection compared to heartbeat-based approaches
- **Implement** the three-phase probe protocol: direct ping, indirect ping through K random members, and suspicion timeout
- **Analyze** how the suspicion sub-protocol reduces false positive rates in the presence of network congestion and asymmetric packet loss
- **Design** a piggybacking dissemination layer that leverages protocol messages to spread membership updates with zero additional network cost
- **Create** a comprehensive test suite that validates failure detection under simulated packet loss, asymmetric partitions, and message delays

## The Challenge

Traditional heartbeat-based failure detectors have a fundamental scaling problem: either every node monitors every other node (O(N^2) messages per round) or a subset of nodes monitor each other (incomplete detection). The SWIM (Scalable Weakly-consistent Infection-style Membership) protocol solves both problems with a constant message load per node per protocol round, regardless of cluster size.

SWIM's insight is to separate failure detection from dissemination. For detection, each node probes one random peer per protocol period using a three-phase approach. Phase 1: send a direct `ping` to the target and wait for an `ack`. If no `ack` arrives within timeout, move to Phase 2: select K random members and send `ping-req` messages asking them to probe the target on your behalf. If any indirect probe returns an `ack`, the target is alive. Phase 3: if neither direct nor indirect probes succeed, mark the target as suspected. After a configurable suspicion timeout with no refutation, declare the target failed.

Dissemination is piggybacked on protocol messages (pings, acks, ping-reqs). Each membership update (join, leave, suspect, fail) is attached to outgoing messages until it has been piggybacked enough times to guarantee cluster-wide delivery with high probability. This eliminates the need for separate broadcast messages.

This is the protocol that HashiCorp's memberlist library implements. Consul, Nomad, and Serf all depend on it for cluster membership. Implementing SWIM teaches you how to build a failure detector that is both scalable and accurate.

Implement the full SWIM protocol: direct probe, indirect probe through K random members, suspicion mechanism with refutation, piggybacked dissemination, and node lifecycle (join, leave, fail).

## Key Concepts

Before implementing, understand these core ideas from the SWIM paper:

**O(1) message load per node.** In each protocol period, a node sends exactly one ping (plus at most K ping-reqs if the direct probe fails). This means each node generates a constant number of messages per round, regardless of cluster size N. The total network load is O(N) per round across the cluster. Compare this to all-to-all heartbeating at O(N^2) -- SWIM is O(N) times more efficient.

**Three-phase probe.** The probe sequence is: direct ping -> indirect ping-req -> suspect. Each phase has a timeout. If the direct ping succeeds, the round completes immediately (one message pair). If it fails, indirect probing adds K message pairs (one to each helper, one from each helper to the target, and the responses). If both fail, the node is suspected. This graduated approach minimizes messages in the common case (healthy nodes) while providing redundant detection in the failure case.

**Suspicion mechanism.** Declaring a node as failed immediately after a missed probe is too aggressive -- network congestion, GC pauses, or CPU spikes can cause transient unresponsiveness. The suspicion sub-protocol adds a grace period: the node is marked as "suspected" and given Tsuspect time to refute. Refutation uses incarnation numbers: the suspected node increments its incarnation counter and disseminates an alive message. Higher incarnation numbers always override lower-incarnation suspicion messages. This is a form of optimistic conflict resolution.

**Incarnation numbers.** Each node maintains a monotonically increasing incarnation number. When node X is suspected with incarnation N, X can refute by disseminating alive(X, incarnation=N+1). Other nodes accept this refutation because the higher incarnation is provably more recent. Without incarnation numbers, a refutation could be reordered with the original suspicion, causing oscillation between alive and suspected states.

**Infection-style dissemination.** The "I" in SWIM stands for infection-style. Membership updates spread by piggybacking on protocol messages, analogous to how a virus spreads through contact. Each update is attached to Lambda * log(N) messages, ensuring cluster-wide delivery with high probability. The log(N) factor comes from the coupon collector problem: to hit all N nodes with random peer selection, you need O(N log N) attempts, distributed across all nodes.

## Requirements

1. Protocol period: every T milliseconds, select one random member and initiate a probe sequence
2. Direct probe: send `ping` to the target, wait up to `Tping` for an `ack`
3. Indirect probe: if direct ping fails, select K random members (excluding self and target) and send `ping-req(target)`. Each selected member forwards a `ping` to the target and relays the `ack` back. Wait up to `Tindirect` for any `ack`
4. Suspicion: if both direct and indirect probes fail, mark the target as `suspected` (not immediately `failed`). Start a suspicion timer of `Tsuspect`. During this period, the suspected node can refute by sending any message (proving it is alive). If the timer expires without refutation, mark as `failed`
5. Suspicion sub-protocol: when a node learns it is suspected, it increments its incarnation number and disseminates an `alive` message with the higher incarnation. Higher incarnation numbers override suspicion. This prevents false failures due to temporary network issues
6. Piggybacked dissemination: maintain a bounded buffer of recent membership events. Attach events (ordered by priority: failed > suspected > alive > join) to every outgoing message (ping, ack, ping-req). Each event tracks a dissemination count; remove it after it has been piggybacked `Lambda * log(N)` times (where Lambda is a configurable multiplier)
7. Node lifecycle: join (via seed node), voluntary leave (disseminates leave status), failure (detected by protocol)
8. Message types: `ping`, `ack`, `ping-req`, `ping-req-ack` (indirect ack relayed back)
9. Incarnation numbers: each node maintains a monotonically increasing incarnation number. An alive message with a higher incarnation overrides any suspect/fail status for that node
10. Network layer: UDP with JSON or binary serialization. Transport interface for simulation testing
11. Metrics: track probes sent (direct, indirect), acks received, false suspicions (refuted), actual failures detected, messages sent/received, average probe round-trip time
12. Configurable parameters: protocol period (T), direct ping timeout (Tping), indirect probe timeout (Tindirect), indirect probe fanout (K), suspicion timeout (Tsuspect), dissemination multiplier (Lambda)

## Hints

1. Structure the probe sequence as a state machine per protocol round: `idle -> direct_ping_sent -> waiting_indirect -> suspected -> confirmed_alive | confirmed_failed`. Use a `select` with `time.After` for each timeout transition. Do not use nested goroutines for the probe stages -- a single goroutine with sequential `select` blocks is cleaner and avoids lifetime management bugs.

2. The indirect probe (`ping-req`) requires three message hops: you send `ping-req(target)` to a helper, the helper sends `ping(target)`, the target sends `ack` to the helper, and the helper sends the `ack` back to you. Implement this by having each `ping` carry the original requester's ID. When a node receives a `ping` with a non-empty requester field, it knows to forward the `ack` to the requester rather than the sender.

3. For the suspicion sub-protocol, store an `incarnation uint32` per member. When a node receives a `suspect(X, incarnation=N)` message, it only transitions X to suspected if N >= the locally known incarnation for X. When X learns it is suspected, it sets `incarnation = max(localIncarnation, N) + 1` and disseminates `alive(X, incarnation=new)`. This is how false positives are corrected without waiting for the full suspicion timeout.

4. The piggybacked dissemination buffer should be a priority queue ordered by: (1) priority (failed events first, then suspected, then alive), (2) dissemination count (least disseminated first). Each outgoing message attaches the top M events from this queue and increments their dissemination counts. Events that reach `Lambda * log(N)` disseminations are removed. The log(N) factor ensures O(N log N) total disseminations, which gives probabilistic delivery to all nodes.

5. For testing, build a `SimTransport` that supports: message delay (buffer messages for a configurable duration before delivery), message loss (drop with probability P), and asymmetric partitions (A can reach B but B cannot reach A). Asymmetric partitions are the hardest case for SWIM: the target can receive indirect pings but not send direct acks to the initiator. The indirect probe path handles this correctly.

6. Measure false positive rate by running the protocol with 0% actual failures and injecting various levels of packet loss (1%, 5%, 10%). With the suspicion sub-protocol enabled, the false positive rate should be near zero even at 10% packet loss for clusters under 50 nodes. Without suspicion, false positives rise proportionally to packet loss rate.

## Acceptance Criteria

- [ ] A stopped node is detected as failed within 2 * T + Tindirect + Tsuspect duration
- [ ] Indirect probes correctly detect a node that is reachable by helpers but not by the initiator (asymmetric partition)
- [ ] Suspicion refutation works: a suspected node that is actually alive refutes with a higher incarnation number and remains in the cluster
- [ ] Piggybacked dissemination delivers membership updates to all nodes within O(log N) protocol rounds
- [ ] Message complexity is O(1) per node per protocol round (each node sends exactly 1 ping + at most K ping-reqs)
- [ ] False positive rate is below 1% with 5% simulated packet loss on a 10-node cluster
- [ ] Node join and voluntary leave propagate to all members within 10 protocol rounds
- [ ] All tests pass with `-race` flag
- [ ] Metrics accurately report: probe counts, ack counts, suspicion events, failure events, average RTT
- [ ] At least 8 test scenarios: normal detection, indirect probe, suspicion refutation, asymmetric partition, concurrent failures, join mid-cluster, voluntary leave, stress with packet loss

## Going Further

- Implement Lifeguard extensions (HashiCorp's improvements to SWIM): dynamic suspicion timeout based on cluster size, local health awareness (if a node suspects it is slow, it extends its own suspicion timeout), and buddy system (nodes with suspected peers probe them more aggressively)
- Add protocol-aware compression: instead of sending full member entries, send deltas (only changed fields since last exchange with this peer)
- Build a service mesh health checker on top of SWIM: each node registers services and SWIM detects both node failures and service failures
- Implement protocol versioning so nodes running different protocol versions can coexist during rolling upgrades

## Starting Points

Study these implementations in order of increasing complexity:

- **hashicorp/memberlist** (`hashicorp/memberlist`): The definitive Go implementation of SWIM+Lifeguard. Study `state.go` for the probe state machine, `net.go` for the UDP transport, and `suspicion.go` for the suspicion timer implementation. The `memberlist_test.go` file shows how to test the protocol with simulated failures.

- **SWIM paper pseudocode**: Sections 3 and 4 of the SWIM paper contain clear pseudocode for the protocol period, ping, ping-req, and failure detection. Translate this pseudocode directly into Go as your starting point. The paper's notation maps directly to function signatures.

- **Consul source** (`hashicorp/consul`): Consul uses memberlist for its cluster membership. Study `agent/consul/server.go` to see how a production system initializes and configures the SWIM protocol. The default configuration values are battle-tested starting points for your implementation.

## Research Resources

- [SWIM: Scalable Weakly-consistent Infection-style Process Group Membership Protocol (Das et al., 2002)](https://www.cs.cornell.edu/projects/Quicksilver/public_pdfs/SWIM.pdf) -- the original SWIM paper, sections 3 and 4 describe the protocol completely
- [Lifeguard: Local Health Awareness for More Accurate Failure Detection (Hashimoto, 2017)](https://arxiv.org/abs/1707.00788) -- HashiCorp's extensions to SWIM that reduce false positives under node overload
- [hashicorp/memberlist](https://github.com/hashicorp/memberlist) -- production Go implementation of SWIM+Lifeguard, used by Consul and Nomad
- [A Swim through the Internals of Serf](https://www.hashicorp.com/blog/making-gossip-more-robust-with-lifeguard) -- HashiCorp's explanation of their SWIM implementation and Lifeguard improvements
- [On Scalable and Efficient Distributed Failure Detectors (Gupta et al., 2001)](https://www.cs.cornell.edu/home/rvr/papers/GossipFD.pdf) -- theoretical foundations for gossip-based failure detection
- [The Phi Accrual Failure Detector (Hayashibara et al., 2004)](https://www.researchgate.net/publication/29682135_The_ph_accrual_failure_detector) -- alternative approach that outputs a suspicion level rather than binary alive/dead, used by Akka and Cassandra
